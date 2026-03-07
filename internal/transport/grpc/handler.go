package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	"github.com/ellipse/kernel-lab/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LabHandler struct {
	pb.UnimplementedLabServiceServer
	provisioner          *docker.Provisioner
	registry             domain.LabRegistry
	containerLabs        sync.Map
	ttlCancels           sync.Map
	containerCreationTTL sync.Map
	containerDeadlines   sync.Map
	containerTTL         time.Duration
	log                  logger.Logger
}

func NewLabHandler(p *docker.Provisioner, r domain.LabRegistry, ttl time.Duration, log logger.Logger) *LabHandler {
	return &LabHandler{provisioner: p, registry: r, containerTTL: ttl, log: log}
}

func (h *LabHandler) RegisterLab(
	_ context.Context,
	req *pb.RegisterLabRequest,
) (*pb.RegisterLabResponse, error) {
	lab := domain.Lab{
		ID:              req.LabId,
		Image:           req.Image,
		InitialCode:     req.InitialCode,
		JudgeCode:       req.JudgeCode,
		JudgeType:       req.JudgeType,
		Limits:          domain.NewResourceLimits(req.CpuLimit, req.RamLimitMb),
		DurationSeconds: req.DurationSeconds,
	}
	if err := h.registry.Register(lab); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "register lab: %v", err)
	}
	h.log.Info("lab registered",
		logger.String("lab_id", req.LabId),
		logger.String("image", req.Image),
		logger.Int64("duration_seconds", req.DurationSeconds),
	)
	return &pb.RegisterLabResponse{Success: true, LabId: req.LabId}, nil
}

func (h *LabHandler) StartLab(
	ctx context.Context,
	req *pb.LabRequest,
) (*pb.LabResponse, error) {
	h.log.InfoContext(ctx, "StartLab requested", logger.String("lab_id", req.LabId))
	lab, err := h.registry.Get(req.LabId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}

	start := time.Now()
	id, err := h.provisioner.Spawn(ctx, lab)
	if err != nil {
		h.log.ErrorContext(ctx, "spawn failed",
			logger.String("lab_id", req.LabId),
			logger.Any("error", err),
		)
		return nil, status.Errorf(codes.Internal, "spawn container: %v", err)
	}

	h.containerLabs.Store(id, lab)

	ttl := h.containerTTL
	if lab.DurationSeconds > 0 {
		ttl = time.Duration(lab.DurationSeconds) * time.Second
	}

	h.containerCreationTTL.Store(id, ttl)
	h.containerDeadlines.Store(id, time.Now().Add(ttl))

	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	h.ttlCancels.Store(id, ttlCancel)
	go h.scheduleCleanup(ttlCtx, id, ttl)

	h.log.InfoContext(ctx, "lab started",
		logger.String("lab_id", req.LabId),
		logger.String("container_id", id),
		logger.Any("took", time.Since(start)),
		logger.Any("ttl", ttl),
	)

	return &pb.LabResponse{
		ContainerId: id,
		InitialCode: lab.InitialCode,
	}, nil
}

func (h *LabHandler) StopLab(
	ctx context.Context,
	req *pb.StopRequest,
) (*pb.StopResponse, error) {
	h.log.InfoContext(ctx, "StopLab requested", logger.String("container_id", req.ContainerId))
	if v, ok := h.ttlCancels.LoadAndDelete(req.ContainerId); ok {
		v.(context.CancelFunc)()
	}
	h.containerCreationTTL.Delete(req.ContainerId)
	h.containerDeadlines.Delete(req.ContainerId)
	if err := h.provisioner.Stop(ctx, req.ContainerId); err != nil {
		return &pb.StopResponse{Success: false},
			status.Errorf(codes.Internal, "stop container: %v", err)
	}
	h.containerLabs.Delete(req.ContainerId)
	return &pb.StopResponse{Success: true}, nil
}

func (h *LabHandler) scheduleCleanup(ctx context.Context, containerID string, ttl time.Duration) {
	select {
	case <-time.After(ttl):
		h.log.Info("TTL expired, stopping container", logger.String("container_id", containerID))
		_ = h.provisioner.Stop(context.Background(), containerID)
		h.containerLabs.Delete(containerID)
		h.ttlCancels.Delete(containerID)
	case <-ctx.Done():
		h.log.Debug("cleanup cancelled", logger.String("container_id", containerID))
	}
}

type streamWriter struct {
	stream grpc.BidiStreamingServer[pb.TerminalInput, pb.TerminalOutput]
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if err := w.stream.Send(&pb.TerminalOutput{Data: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (h *LabHandler) TerminalStream(
	stream grpc.BidiStreamingServer[pb.TerminalInput, pb.TerminalOutput],
) error {
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "expected initial message: %v", err)
	}
	containerID := first.ContainerId
	if containerID == "" {
		return status.Error(codes.InvalidArgument, "container_id must be set in the first message")
	}

	h.log.InfoContext(stream.Context(), "terminal stream opened", logger.String("container_id", containerID))

	pr, pw := io.Pipe()
	resizeCh := make(chan domain.ResizeEvent, 4)

	go func() {
		defer pw.Close()
		defer close(resizeCh)

		for _, msg := range []*pb.TerminalInput{first, nil} {
			if msg == nil {
				break
			}
			h.dispatchTerminalMsg(msg, pw, resizeCh, containerID)
		}

		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				if recvErr != io.EOF {
					h.log.WarnContext(stream.Context(), "terminal recv error",
						logger.String("container_id", containerID),
						logger.Any("error", recvErr),
					)
				}
				return
			}
			h.dispatchTerminalMsg(msg, pw, resizeCh, containerID)
		}
	}()

	rw := struct {
		io.Reader
		io.Writer
	}{Reader: pr, Writer: &streamWriter{stream: stream}}

	if err := h.provisioner.StreamTerminal(stream.Context(), containerID, &rw, resizeCh); err != nil {
		h.log.InfoContext(stream.Context(), "terminal stream closed",
			logger.String("container_id", containerID),
			logger.Any("error", err),
		)
		return nil
	}

	h.log.InfoContext(stream.Context(), "terminal stream closed", logger.String("container_id", containerID))
	return nil
}

func (h *LabHandler) dispatchTerminalMsg(msg *pb.TerminalInput, pw *io.PipeWriter, resizeCh chan<- domain.ResizeEvent, containerID string) {
	if msg.Cols > 0 && msg.Rows > 0 {
		resizeCh <- domain.ResizeEvent{Cols: uint(msg.Cols), Rows: uint(msg.Rows)}
	}
	if len(msg.Data) > 0 {
		if _, err := pw.Write(msg.Data); err != nil {
			h.log.Warn("terminal pipe write error",
				logger.String("container_id", containerID),
				logger.Any("error", err),
			)
		}
	}
}

func (h *LabHandler) ExecCheck(
	ctx context.Context,
	req *pb.ExecRequest,
) (*pb.ExecResponse, error) {
	h.log.InfoContext(ctx, "ExecCheck requested", logger.String("container_id", req.ContainerId))
	rawLab, ok := h.containerLabs.Load(req.ContainerId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no lab found for container %q", req.ContainerId)
	}
	lab := rawLab.(domain.Lab)

	if err := h.provisioner.CopyToContainer(ctx, req.ContainerId, "/tmp", "solution", []byte(req.Code)); err != nil {
		return nil, status.Errorf(codes.Internal, "copy code to container: %v", err)
	}

	judgeCmd := lab.JudgeCode
	if judgeCmd == "" {
		judgeCmd = "sh /tmp/solution"
	}

	execCtx := ctx
	timeoutSecs := int64(30)
	if req.TimeoutSeconds > 0 {
		timeoutSecs = int64(req.TimeoutSeconds)
	}
	var cancel context.CancelFunc
	execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	result, err := h.provisioner.Exec(execCtx, req.ContainerId, []string{"sh", "-c", judgeCmd})
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return &pb.ExecResponse{
				Stdout:   result.Stdout,
				Stderr:   "execution timed out after " + fmt.Sprintf("%d", timeoutSecs) + " seconds",
				ExitCode: 124,
			}, nil
		}
		return nil, status.Errorf(codes.Internal, "exec check: %v", err)
	}

	h.log.InfoContext(ctx, "ExecCheck done",
		logger.String("container_id", req.ContainerId),
		logger.Int("exit_code", int(result.ExitCode)),
	)

	return &pb.ExecResponse{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: int32(result.ExitCode),
	}, nil
}

func (h *LabHandler) UploadFile(
	ctx context.Context,
	req *pb.UploadFileRequest,
) (*pb.UploadFileResponse, error) {
	h.log.InfoContext(ctx, "UploadFile requested",
		logger.String("container_id", req.ContainerId),
		logger.String("dest_path", req.DestPath),
		logger.String("filename", req.Filename),
	)

	if _, ok := h.containerLabs.Load(req.ContainerId); !ok {
		return &pb.UploadFileResponse{
			Success:  false,
			ErrorMsg: fmt.Sprintf("container %q not found", req.ContainerId),
		}, nil
	}

	if err := h.provisioner.UploadFile(ctx, req.ContainerId, req.DestPath, req.Filename, req.Content); err != nil {
		h.log.ErrorContext(ctx, "upload file failed",
			logger.String("container_id", req.ContainerId),
			logger.Any("error", err),
		)
		return &pb.UploadFileResponse{
			Success:  false,
			ErrorMsg: err.Error(),
		}, nil
	}

	h.log.InfoContext(ctx, "file uploaded successfully",
		logger.String("container_id", req.ContainerId),
		logger.String("filename", req.Filename),
	)
	return &pb.UploadFileResponse{
		Success:  true,
		ErrorMsg: "",
	}, nil
}

func (h *LabHandler) ExtendLab(
	ctx context.Context,
	req *pb.ExtendLabRequest,
) (*pb.ExtendLabResponse, error) {
	containerID := req.ContainerId
	extendSecs := req.ExtendSeconds

	h.log.InfoContext(ctx, "ExtendLab requested",
		logger.String("container_id", containerID),
		logger.Int64("extend_seconds", extendSecs),
	)

	if _, ok := h.containerLabs.Load(containerID); !ok {
		return &pb.ExtendLabResponse{
			Success:  false,
			ErrorMsg: fmt.Sprintf("container %q not found", containerID),
		}, nil
	}

	if extendSecs <= 0 {
		return &pb.ExtendLabResponse{
			Success:  false,
			ErrorMsg: "extend_seconds must be positive",
		}, nil
	}

	deadline, ok := h.containerDeadlines.Load(containerID)
	if !ok {
		return &pb.ExtendLabResponse{
			Success:  false,
			ErrorMsg: "deadline not found for container",
		}, nil
	}

	oldDeadline := deadline.(time.Time)
	newDeadline := oldDeadline.Add(time.Duration(extendSecs) * time.Second)
	remainingTime := time.Until(newDeadline)

	if remainingTime <= 0 {
		return &pb.ExtendLabResponse{
			Success:  false,
			ErrorMsg: "container would expire immediately after extension",
		}, nil
	}

	if v, ok := h.ttlCancels.LoadAndDelete(containerID); ok {
		v.(context.CancelFunc)()
	}

	h.containerDeadlines.Store(containerID, newDeadline)

	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	h.ttlCancels.Store(containerID, ttlCancel)
	go h.scheduleCleanup(ttlCtx, containerID, remainingTime)

	h.log.InfoContext(ctx, "lab extended",
		logger.String("container_id", containerID),
		logger.Any("old_deadline", oldDeadline),
		logger.Any("new_deadline", newDeadline),
		logger.Int64("remaining_seconds", int64(remainingTime.Seconds())),
	)

	return &pb.ExtendLabResponse{
		Success:       true,
		NewTtlSeconds: int64(remainingTime.Seconds()),
		ErrorMsg:      "",
	}, nil
}

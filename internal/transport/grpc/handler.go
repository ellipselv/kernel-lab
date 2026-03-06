package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LabHandler struct {
	pb.UnimplementedLabServiceServer
	provisioner   *docker.Provisioner
	registry      domain.LabRegistry
	containerLabs sync.Map
	ttlCancels    sync.Map
	containerTTL  time.Duration
	log           *slog.Logger
}

func NewLabHandler(p *docker.Provisioner, r domain.LabRegistry, ttl time.Duration, log *slog.Logger) *LabHandler {
	return &LabHandler{provisioner: p, registry: r, containerTTL: ttl, log: log}
}

func (h *LabHandler) RegisterLab(
	_ context.Context,
	req *pb.RegisterLabRequest,
) (*pb.RegisterLabResponse, error) {
	lab := domain.Lab{
		ID:          req.LabId,
		Image:       req.Image,
		InitialCode: req.InitialCode,
		JudgeCode:   req.JudgeCode,
		JudgeType:   req.JudgeType,
		Limits:      domain.NewResourceLimits(req.CpuLimit, req.RamLimitMb),
	}
	if err := h.registry.Register(lab); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "register lab: %v", err)
	}
	h.log.Info("lab registered",
		slog.String("lab_id", req.LabId),
		slog.String("image", req.Image),
	)
	return &pb.RegisterLabResponse{Success: true, LabId: req.LabId}, nil
}

func (h *LabHandler) StartLab(
	ctx context.Context,
	req *pb.LabRequest,
) (*pb.LabResponse, error) {
	h.log.InfoContext(ctx, "StartLab requested", slog.String("lab_id", req.LabId))
	lab, err := h.registry.Get(req.LabId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}

	start := time.Now()
	id, err := h.provisioner.Spawn(ctx, lab)
	if err != nil {
		h.log.ErrorContext(ctx, "spawn failed",
			slog.String("lab_id", req.LabId),
			slog.Any("error", err),
		)
		return nil, status.Errorf(codes.Internal, "spawn container: %v", err)
	}

	h.containerLabs.Store(id, lab)

	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	h.ttlCancels.Store(id, ttlCancel)
	go h.scheduleCleanup(ttlCtx, id)

	h.log.InfoContext(ctx, "lab started",
		slog.String("lab_id", req.LabId),
		slog.String("container_id", id),
		slog.Duration("took", time.Since(start)),
		slog.Duration("ttl", h.containerTTL),
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
	h.log.InfoContext(ctx, "StopLab requested", slog.String("container_id", req.ContainerId))
	if v, ok := h.ttlCancels.LoadAndDelete(req.ContainerId); ok {
		v.(context.CancelFunc)()
	}
	if err := h.provisioner.Stop(ctx, req.ContainerId); err != nil {
		return &pb.StopResponse{Success: false},
			status.Errorf(codes.Internal, "stop container: %v", err)
	}
	h.containerLabs.Delete(req.ContainerId)
	return &pb.StopResponse{Success: true}, nil
}

func (h *LabHandler) scheduleCleanup(ctx context.Context, containerID string) {
	select {
	case <-time.After(h.containerTTL):
		h.log.Info("TTL expired, stopping container", slog.String("container_id", containerID))
		_ = h.provisioner.Stop(context.Background(), containerID)
		h.containerLabs.Delete(containerID)
		h.ttlCancels.Delete(containerID)
	case <-ctx.Done():
		// Explicit StopLab already handled cleanup.
	}
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

	h.log.InfoContext(stream.Context(), "terminal stream opened", slog.String("container_id", containerID))

	stdin, stdout, cleanup, err := h.provisioner.Attach(stream.Context(), containerID)
	if err != nil {
		return status.Errorf(codes.Internal, "attach to container: %v", err)
	}
	defer cleanup()

	outErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.TerminalOutput{Data: buf[:n]}); sendErr != nil {
					outErr <- sendErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					outErr <- fmt.Errorf("stdout read: %w", err)
				} else {
					outErr <- nil
				}
				return
			}
		}
	}()

	if len(first.Data) > 0 {
		if _, err := stdin.Write(first.Data); err != nil {
			return status.Errorf(codes.Internal, "write initial data: %v", err)
		}
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			h.log.WarnContext(stream.Context(), "terminal stream recv error",
				slog.String("container_id", containerID),
				slog.Any("error", err),
			)
			break
		}
		if _, err := stdin.Write(msg.Data); err != nil {
			h.log.WarnContext(stream.Context(), "terminal stdin write error",
				slog.String("container_id", containerID),
				slog.Any("error", err),
			)
			break
		}
	}

	h.log.InfoContext(stream.Context(), "terminal stream closed", slog.String("container_id", containerID))
	stdin.Close()
	return <-outErr
}

func (h *LabHandler) ExecCheck(
	ctx context.Context,
	req *pb.ExecRequest,
) (*pb.ExecResponse, error) {
	h.log.InfoContext(ctx, "ExecCheck requested", slog.String("container_id", req.ContainerId))
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

	result, err := h.provisioner.Exec(ctx, req.ContainerId, []string{"sh", "-c", judgeCmd})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "exec check: %v", err)
	}

	h.log.InfoContext(ctx, "ExecCheck done",
		slog.String("container_id", req.ContainerId),
		slog.Int("exit_code", int(result.ExitCode)),
	)

	return &pb.ExecResponse{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: int32(result.ExitCode),
	}, nil
}

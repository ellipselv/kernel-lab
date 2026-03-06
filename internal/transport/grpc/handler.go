package grpc

import (
	"context"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
)

type LabHandler struct {
	pb.UnimplementedLabServiceServer
	provisioner *docker.Provisioner
}

func NewLabHandler(p *docker.Provisioner) *LabHandler {
	return &LabHandler{provisioner: p}
}

func (h *LabHandler) StartLab(ctx context.Context, req *pb.LabRequest) (*pb.LabResponse, error) {
	lab := domain.Lab{
		Image:  req.Image,
		Limits: domain.NewResourceLimits(req.CpuLimit, req.RamLimitMb),
	}

	id, err := h.provisioner.GetFromPool(ctx, lab)
	if err != nil {
		return nil, err
	}

	return &pb.LabResponse{ContainerId: id}, nil
}

func (h *LabHandler) StopLab(ctx context.Context, req *pb.StopRequest) (*pb.StopResponse, error) {
	err := h.provisioner.Stop(ctx, req.ContainerId)
	if err != nil {
		return &pb.StopResponse{Success: false}, err
	}
	return &pb.StopResponse{Success: true}, nil
}

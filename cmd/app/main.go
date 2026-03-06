package main

import (
	"log/slog"
	"net"
	"os"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	labGrpc "github.com/ellipse/kernel-lab/internal/transport/grpc"
	"google.golang.org/grpc"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(log)

	p, err := docker.NewProvisioner(log)
	if err != nil {
		log.Error("failed to connect to docker", slog.Any("error", err))
		os.Exit(1)
	}
	defer p.Close()

	// Registry starts empty.
	// Labs are registered at runtime via the RegisterLab RPC.
	registry := domain.NewInMemoryRegistry()

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Error("failed to listen", slog.Any("error", err))
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterLabServiceServer(s, labGrpc.NewLabHandler(p, registry, 30*time.Minute, log))

	log.Info("server listening", slog.String("addr", ":50051"))
	if err := s.Serve(lis); err != nil {
		log.Error("server stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

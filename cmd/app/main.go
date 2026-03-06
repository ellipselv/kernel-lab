package main

import (
	"context"
	"fmt"
	"net"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	labGrpc "github.com/ellipse/kernel-lab/internal/transport/grpc"
	"google.golang.org/grpc"
)

func main() {
	p, err := docker.NewProvisioner()
	if err != nil {
		panic(fmt.Sprintf("failed to connect to docker: &v", err))
	}
	defer p.Close()

	defaultLab := domain.Lab{
		Image:  "tinygo/tinygo:0.40.1",
		Limits: domain.NewResourceLimits(0.5, 256),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fmt.Println("Warming up pool...")
	p.InitPool(ctx, defaultLab, 5)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		panic(err)
	}

	s := grpc.NewServer()
	pb.RegisterLabServiceServer(s, labGrpc.NewLabHandler(p))

	fmt.Println("Server is running on port 50051...")
	if err := s.Serve(lis); err != nil {
		panic(err)
	}
}

package main

import (
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
		panic(fmt.Sprintf("failed to connect to docker: %v", err))
	}
	defer p.Close()

	registry := domain.NewInMemoryRegistry()

	defaultLab := domain.Lab{
		ID:    "tinygo-intro",
		Image: "tinygo/tinygo:0.40.1",
		InitialCode: `package main

import "fmt"

func main() {
	fmt.Println("Hello, TinyGo!")
}
`,
		JudgeCode: `cd /tmp && tinygo run solution`,
		JudgeType: "script",
		Limits:    domain.NewResourceLimits(0.5, 256),
	}
	if err := registry.Register(defaultLab); err != nil {
		panic(fmt.Sprintf("failed to register default lab: %v", err))
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		panic(err)
	}

	s := grpc.NewServer()
	pb.RegisterLabServiceServer(s, labGrpc.NewLabHandler(p, registry, 30*time.Minute))

	fmt.Println("Server is running on :50051 …")
	if err := s.Serve(lis); err != nil {
		panic(err)
	}
}

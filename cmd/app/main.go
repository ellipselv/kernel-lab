package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	labGrpc "github.com/ellipse/kernel-lab/internal/transport/grpc"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	const grpcAddr = ":50051"
	const httpAddr = ":8080"

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

	registry := domain.NewInMemoryRegistry()

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("failed to listen", slog.Any("error", err))
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterLabServiceServer(s, labGrpc.NewLabHandler(p, registry, 30*time.Minute, log))

	gatewayMux := runtime.NewServeMux()
	if err := pb.RegisterLabServiceHandlerFromEndpoint(context.Background(), gatewayMux, grpcAddr, []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}); err != nil {
		log.Error("failed to register gateway handler", slog.Any("error", err))
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: gatewayMux,
	}

	errCh := make(chan error, 2)

	go func() {
		log.Info("gRPC server listening", slog.String("addr", grpcAddr))
		errCh <- s.Serve(lis)
	}()

	go func() {
		log.Info("HTTP gateway listening", slog.String("addr", httpAddr))
		errCh <- httpSrv.ListenAndServe()
	}()

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

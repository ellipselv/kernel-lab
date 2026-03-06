package grpc_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/ellipse/kernel-lab/api/proto"
	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
	labGrpc "github.com/ellipse/kernel-lab/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20 // 1 MiB

func newTestServer(t *testing.T) (pb.LabServiceClient, func()) {
	t.Helper()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p, err := docker.NewProvisioner(log)
	if err != nil {
		t.Fatalf("docker provisioner: %v", err)
	}

	registry := domain.NewInMemoryRegistry()
	handler := labGrpc.NewLabHandler(p, registry, 5*time.Minute, log)

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	pb.RegisterLabServiceServer(srv, handler)

	go func() { _ = srv.Serve(lis) }()

	dialCtx := context.Background()
	conn, err := grpc.DialContext(dialCtx, "bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}

	cleanup := func() {
		conn.Close()
		srv.GracefulStop()
		p.Close()
	}

	return pb.NewLabServiceClient(conn), cleanup
}

func TestIntegration_RegisterStartExecStop(t *testing.T) {
	client, cleanup := newTestServer(t)
	defer cleanup()

	ctx := context.Background()

	_, err := client.RegisterLab(ctx, &pb.RegisterLabRequest{
		LabId:       "tinygo-intro",
		Image:       "alpine:3.21",
		InitialCode: "package main\n\nfunc main() {}\n",
		JudgeCode:   "echo PASS",
		JudgeType:   "sh",
		CpuLimit:    0.5,
		RamLimitMb:  256,
	})
	if err != nil {
		t.Fatalf("RegisterLab: %v", err)
	}

	startResp, err := client.StartLab(ctx, &pb.LabRequest{LabId: "tinygo-intro"})
	if err != nil {
		t.Fatalf("StartLab: %v", err)
	}
	containerID := startResp.ContainerId
	t.Logf("container: %s", containerID)

	execResp, err := client.ExecCheck(ctx, &pb.ExecRequest{
		ContainerId: containerID,
		Code:        "package main\n\nfunc main() {}\n",
	})
	if err != nil {
		t.Fatalf("ExecCheck: %v", err)
	}
	t.Logf("exit_code=%d stdout=%q stderr=%q", execResp.ExitCode, execResp.Stdout, execResp.Stderr)

	if execResp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", execResp.ExitCode)
	}
	if !strings.Contains(execResp.Stdout, "PASS") {
		t.Errorf("expected stdout to contain PASS, got %q", execResp.Stdout)
	}

	stopResp, err := client.StopLab(ctx, &pb.StopRequest{ContainerId: containerID})
	if err != nil {
		t.Fatalf("StopLab: %v", err)
	}
	if !stopResp.Success {
		t.Errorf("StopLab returned success=false")
	}
}

func TestIntegration_RegisterLab_Duplicate(t *testing.T) {
	client, cleanup := newTestServer(t)
	defer cleanup()

	ctx := context.Background()

	req := &pb.RegisterLabRequest{
		LabId:      "dup-lab",
		Image:      "alpine:3.21",
		JudgeCode:  "echo OK",
		JudgeType:  "sh",
		CpuLimit:   0.5,
		RamLimitMb: 128,
	}

	if _, err := client.RegisterLab(ctx, req); err != nil {
		t.Fatalf("first RegisterLab: %v", err)
	}

	_, err := client.RegisterLab(ctx, req)
	if err == nil {
		t.Fatal("expected error on duplicate registration, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestIntegration_StartLab_UnknownID(t *testing.T) {
	client, cleanup := newTestServer(t)
	defer cleanup()

	ctx := context.Background()

	_, err := client.StartLab(ctx, &pb.LabRequest{LabId: "does-not-exist"})
	if err == nil {
		t.Fatal("expected error for unknown lab ID, got nil")
	}
	t.Logf("got expected error: %v", err)
}

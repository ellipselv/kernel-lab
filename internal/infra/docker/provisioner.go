package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Provisioner struct {
	api *client.Client
	log *slog.Logger
}

func NewProvisioner(log *slog.Logger) (*Provisioner, error) {
	apiClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Provisioner{api: apiClient, log: log}, nil
}

// Spawn pulls the image (if not already present) then creates and starts a
// container. Because the image is cached on subsequent calls this takes
// roughly 200–500 ms.
func (p *Provisioner) Spawn(ctx context.Context, lab domain.Lab) (string, error) {
	if _, err := p.api.ImageInspect(ctx, lab.Image); err != nil {
		p.log.InfoContext(ctx, "image not found locally, pulling", slog.String("image", lab.Image))
		start := time.Now()
		reader, err := p.api.ImagePull(ctx, lab.Image, client.ImagePullOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to pull image %q: %w", lab.Image, err)
		}
		defer reader.Close()
		io.Copy(io.Discard, reader)
		p.log.InfoContext(ctx, "image pulled",
			slog.String("image", lab.Image),
			slog.Duration("took", time.Since(start)),
		)
	} else {
		p.log.DebugContext(ctx, "image already present, skipping pull", slog.String("image", lab.Image))
	}

	start := time.Now()
	resp, err := p.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: lab.Image,
		Config: &container.Config{
			Cmd: []string{"sh", "-c", "trap : TERM INT; sleep infinity & wait"},
			Tty: true,
		},
		HostConfig: &container.HostConfig{
			AutoRemove: true,
			Resources: container.Resources{
				NanoCPUs: lab.ToCore(),
				Memory:   lab.ToMB(),
			},
		},
	})
	if err != nil {
		return "", err
	}

	if _, err := p.api.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return "", err
	}
	p.log.InfoContext(ctx, "container started",
		slog.String("container_id", resp.ID),
		slog.String("lab_id", lab.ID),
		slog.String("image", lab.Image),
		slog.Duration("took", time.Since(start)),
	)
	return resp.ID, nil
}

func (p *Provisioner) Stop(ctx context.Context, id string) error {
	p.log.InfoContext(ctx, "stopping container", slog.String("container_id", id))
	if _, err := p.api.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
		p.log.ErrorContext(ctx, "failed to stop container",
			slog.String("container_id", id),
			slog.Any("error", err),
		)
		return fmt.Errorf("failed to stop container %s: %w", id, err)
	}
	p.log.InfoContext(ctx, "container stopped", slog.String("container_id", id))
	return nil
}

func (p *Provisioner) Exec(ctx context.Context, id string, cmd []string) (domain.ExecResult, error) {
	p.log.DebugContext(ctx, "exec start",
		slog.String("container_id", id),
		slog.Any("cmd", cmd),
	)
	start := time.Now()
	created, err := p.api.ExecCreate(ctx, id, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          false,
	})
	if err != nil {
		return domain.ExecResult{}, fmt.Errorf("exec create: %w", err)
	}

	ar, err := p.api.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: false})
	if err != nil {
		return domain.ExecResult{}, fmt.Errorf("exec attach: %w", err)
	}
	defer ar.Close()

	var stdout, stderr bytes.Buffer
	if err := demuxDockerStream(ar.Reader, &stdout, &stderr); err != nil {
		return domain.ExecResult{}, fmt.Errorf("exec read: %w", err)
	}

	insp, err := p.api.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return domain.ExecResult{}, fmt.Errorf("exec inspect: %w", err)
	}

	res := domain.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: insp.ExitCode,
	}
	p.execDone(ctx, id, res, time.Since(start))
	return res, nil
}

func (p *Provisioner) execDone(ctx context.Context, id string, res domain.ExecResult, took time.Duration) {
	p.log.InfoContext(ctx, "exec done",
		slog.String("container_id", id),
		slog.Int("exit_code", res.ExitCode),
		slog.Duration("took", took),
	)
}

func (p *Provisioner) Attach(ctx context.Context, id string) (io.WriteCloser, io.Reader, func(), error) {
	p.log.DebugContext(ctx, "attaching PTY", slog.String("container_id", id))
	created, err := p.api.ExecCreate(ctx, id, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("attach exec create: %w", err)
	}

	ar, err := p.api.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("attach exec attach: %w", err)
	}
	p.log.InfoContext(ctx, "PTY attached", slog.String("container_id", id))
	return ar.Conn, ar.Reader, ar.Close, nil
}

func (p *Provisioner) CopyToContainer(
	ctx context.Context,
	id, destPath, filename string,
	content []byte,
) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: filename,
		Mode: 0644,
		Size: int64(len(content)),
	}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	tw.Close()

	_, err := p.api.CopyToContainer(ctx, id, client.CopyToContainerOptions{
		DestinationPath: destPath,
		Content:         &buf,
	})
	return err
}

func demuxDockerStream(r io.Reader, stdout, stderr io.Writer) error {
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		var dst io.Writer
		switch header[0] {
		case 1:
			dst = stdout
		case 2:
			dst = stderr
		default:
			dst = io.Discard
		}
		if _, err := io.CopyN(dst, r, int64(size)); err != nil {
			return err
		}
	}
}

func (p *Provisioner) Close() error {
	return p.api.Close()
}

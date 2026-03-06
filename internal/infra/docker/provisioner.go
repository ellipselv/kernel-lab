package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Provisioner struct {
	api    *client.Client
	warmID chan string
}

func NewProvisioner() (*Provisioner, error) {
	apiClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Provisioner{api: apiClient}, nil
}

func (p *Provisioner) InitPool(ctx context.Context, lab domain.Lab, size int) {
	p.warmID = make(chan string, size)
	for i := 0; i < size; i++ {
		go func() {
			id, err := p.Spawn(ctx, lab)
			if err == nil {
				p.warmID <- id
			}
		}()
	}
}

func (p *Provisioner) GetFromPool(ctx context.Context, lab domain.Lab) (string, error) {
	select {
	case id := <-p.warmID:
		go func() {
			newCtx := context.Background()
			if newID, err := p.Spawn(newCtx, lab); err == nil {
				p.warmID <- newID
			}
		}()
		return id, nil
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return p.Spawn(ctx, lab)
	}
}

func (p *Provisioner) Spawn(ctx context.Context, lab domain.Lab) (string, error) {
	if _, err := p.api.ImageInspect(ctx, lab.Image); err != nil {
		reader, err := p.api.ImagePull(ctx, lab.Image, client.ImagePullOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to pull image %q: %w", lab.Image, err)
		}
		defer reader.Close()
		io.Copy(io.Discard, reader)
	}

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
	return resp.ID, nil
}

func (p *Provisioner) Stop(ctx context.Context, id string) error {
	if _, err := p.api.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", id, err)
	}
	return nil
}

func (p *Provisioner) Exec(ctx context.Context, id string, cmd []string) (domain.ExecResult, error) {
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

	return domain.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: insp.ExitCode,
	}, nil
}

func (p *Provisioner) Attach(ctx context.Context, id string) (io.WriteCloser, io.Reader, func(), error) {
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

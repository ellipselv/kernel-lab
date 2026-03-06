package docker

import (
	"context"
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

func (p *Provisioner) Spawn(ctx context.Context, lab domain.Lab) (string, error) {
	_, err := p.api.ImageInspect(ctx, lab.Image)
	if err != nil {
		reader, err := p.api.ImagePull(ctx, lab.Image, client.ImagePullOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to pull image: %w", err)
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

func (p *Provisioner) Close() error {
	return p.api.Close()
}

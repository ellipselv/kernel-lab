package docker

import (
	"context"
	"io"
	"os"

	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Provisioner struct {
	api *client.Client
}

func NewProvisioner() (*Provisioner, error) {
	apiClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Provisioner{api: apiClient}, nil
}

func (p *Provisioner) Spawn(ctx context.Context, lab domain.Lab) (string, error) {
	reader, err := p.api.ImagePull(ctx, lab.Image, client.ImagePullOptions{})
	if err != nil {
		return "", err
	}
	defer reader.Close()
	io.Copy(os.Stdout, reader)

	resp, err := p.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: lab.Image,
		Config: &container.Config{
			Cmd: []string{"sleep", "100"}, // sleep
		},
		HostConfig: &container.HostConfig{
			// AutoRemove: true,
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

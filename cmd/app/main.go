package main

import (
	"context"
	"io"
	"os"

	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func main() {
	lab := domain.Lab{
		Image:          "tinygo/tinygo:0.40.1",
		ResourceLimits: domain.NewResourceLimits(0.5, 256),
	}

	ctx := context.Background()
	apiClient, err := client.New(client.FromEnv)
	if err != nil {
		panic(err)
	}
	defer apiClient.Close()

	reader, err := apiClient.ImagePull(ctx, lab.Image, client.ImagePullOptions{})
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, reader)

	resp, err := apiClient.ContainerCreate(
		ctx, client.ContainerCreateOptions{
			Image: lab.Image,
			Config: &container.Config{
				Cmd: []string{"tinygo", "version"},
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
		panic(err)
	}

	if _, err := apiClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		panic(err)
	}

	wait := apiClient.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{})
	select {
	case err := <-wait.Error:
		if err != nil {
			panic(err)
		}
	case <-wait.Result:
	}

	out, err := apiClient.ContainerLogs(ctx, resp.ID, client.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	stdcopy.StdCopy(os.Stdout, os.Stderr, out)
}

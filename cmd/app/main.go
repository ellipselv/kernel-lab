package main

import (
	"context"
	"io"
	"os"

	"github.com/ellipse/kernel-lab/internal/domain"

	// "github.com/moby/moby/api/pkg/stdcopy"
	// "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func main() {
	lab := domain.Lab{
		Image: "tinygo/tinygo:0.40.1",
		// CPULimit: 256,
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
}

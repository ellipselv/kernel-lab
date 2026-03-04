package main

import (
	"context"
	"fmt"

	"github.com/ellipse/kernel-lab/internal/domain"
	"github.com/ellipse/kernel-lab/internal/infra/docker"
)

func main() {
	lab := domain.Lab{
		Image:  "tinygo/tinygo:0.40.1",
		Limits: domain.NewResourceLimits(0.5, 256),
	}

	ctx := context.Background()

	p, err := docker.NewProvisioner()
	if err != nil {
		panic(err)
	}

	containerID, err := p.Spawn(ctx, lab)
	if err != nil {
		panic(err)
	}
	fmt.Println(containerID)
}

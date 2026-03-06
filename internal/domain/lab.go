package domain

import (
	"context"
	"io"
)

type Lab struct {
	ID          string          `json:"id"`
	Image       string          `json:"image"`
	InitialCode string          `json:"initial_code"`
	MountPath   string          `json:"mount_path"`
	JudgeCode   string          `json:"judge_code"`
	JudgeType   string          `json:"judge_type"`
	Limits      *ResourceLimits `json:"limits"`
}

type ResourceLimits struct {
	// CPULimit: 0.5 = half a core, 2.0 = two cores.
	CPULimit float64 `json:"cpu_limit"`
	// RAMLimit in megabytes: 256, 512, etc.
	RAMLimit int64 `json:"ram_limit"`
}

func (l *Lab) ToCore() int64 {
	if l.Limits == nil {
		return 0
	}
	return int64(l.Limits.CPULimit * 1e9)
}

func (l *Lab) ToMB() int64 {
	if l.Limits == nil {
		return 0
	}
	return l.Limits.RAMLimit * 1024 * 1024
}

func NewResourceLimits(cpu float64, ramMB int64) *ResourceLimits {
	return &ResourceLimits{CPULimit: cpu, RAMLimit: ramMB}
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Provisioner interface {
	Spawn(ctx context.Context, lab Lab) (containerID string, err error)
	GetFromPool(ctx context.Context, lab Lab) (containerID string, err error)
	Stop(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, cmd []string) (ExecResult, error)
	Attach(ctx context.Context, id string) (io.WriteCloser, io.Reader, func(), error)
}

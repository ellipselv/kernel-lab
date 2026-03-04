package domain

import "context"

type Lab struct {
	// Docker-image (e.g. tinygo/tinygo:0.40.1)
	Image       string          `json:"image"`
	InitialCode string          `json:"initial_code"`
	MountPath   string          `json:"mount_path"`
	JudgeCode   string          `json:"judge_code"`
	JudgeType   string          `json:"judge_type"`
	Limits      *ResourceLimits `json:"limits"`
}

type ResourceLimits struct {
	// CPULimits 0.5 = half a core, 2.0 = two cores.
	CPULimit float64 `json:"cpu_limit"`
	// RAMLimit in mega bytes: 256, 512, etc.
	RAMLimit int64 `json:"ram_limit"`
}

func (rl *Lab) ToCore() int64 {
	return int64(rl.Limits.CPULimit * 1e9)
}

func (rl *Lab) ToMB() int64 {
	return rl.Limits.RAMLimit * 1024 * 1024
}

func NewResourceLimits(cpu float64, ramMB int64) *ResourceLimits {
	return &ResourceLimits{
		CPULimit: cpu,
		RAMLimit: ramMB,
	}
}

type Provisioner interface {
	Spawn(ctx context.Context, lab Lab) (string, error)
	Stop(ctx context.Context, id string) error
}

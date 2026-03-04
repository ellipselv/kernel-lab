package domain

type Lab struct {
	// Docker-image (e.g. tinygo/tinygo:0.40.1)
	Image       string `json:"image"`
	InitialCode string `json:"initial_code"`
	MountPath   string `json:"mount_path"`
	*ResourceLimits
}

type ResourceLimits struct {
	// CPULimits 0.5 = half a core, 2.0 = two cores.
	CPULimit float64 `json:"cpu_limit"`
	// RAMLimit in mega bytes: 256, 512, etc.
	RAMLimit int64 `json:"ram_limit"`
}

func (rl *ResourceLimits) ToCore() int64 {
	return int64(rl.CPULimit * 1e9)
}

func (rl *ResourceLimits) ToMB() int64 {
	return rl.RAMLimit * 1024 * 1024
}

func NewResourceLimits(cpu float64, ramMB int64) *ResourceLimits {
	return &ResourceLimits{
		CPULimit: cpu,
		RAMLimit: ramMB,
	}
}

package domain

type Lab struct {
	// Docker-image (e.g. tinygo/tinygo:0.40.1)
	Image string `json:"image"`
	*ResourceLimits
}

type ResourceLimits struct {
	// CPULimits 0.5 = half a core, 2.0 = two cores.
	CPULimit float64 `json:"cpu_limit"`
	// RAMLimit in mega bytes: 256, 512, etc.
	RAMLimit int64 `json:"ram_limit"`
}

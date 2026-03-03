package domain

type Lab struct {
	Image    string `json:"image"`     // Docker-image (e.g. tinygo/tinygo:0.40.1)
	CPULimit int    `json:"cpu_limit"` // (e.g. 256)
}

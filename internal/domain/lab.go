package domain

type Lab struct {
	Image string `json:"image"` // Docker-image (e.g. tinygo/tinygo:0.40.1)
	Limit string `json:"limit"` // cpu limit
}

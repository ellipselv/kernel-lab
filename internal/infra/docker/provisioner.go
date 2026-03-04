package docker

import "github.com/moby/moby/client"

type Provisioner struct {
	api *client.Client
}

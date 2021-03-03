package main

import (
	"context"
	"fmt"
	"time"

	"github.com/containerd/containerd"
	//"github.com/containerd/containerd/pkg/cri"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type healthReport struct {
	containerRuntime []containerRuntimeHealth
}

type containerRuntimeHealth struct {
	runtimeType string // e.g. docker, containerd
	status      string
	message     string
}

func main() {
	ctx := context.Background()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}

	containerdCli, err := containerd.New("/var//run/containerd/containerd.sock")
	if err != nil {
		panic(err)
	}

	// todo - make this a channel or something so that it doesn't busy wait
	for {
		time.Sleep(time.Second * 5)
		fmt.Println("running health check")

		// Docker daemon connectivity
		dockerHealth := containerRuntimeHealth{
			runtimeType: "docker",
		}
		_, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{})
		if err != nil {
			dockerHealth.status = "impaired"
			dockerHealth.message = fmt.Sprintf("docker runtime health check failed: %+v", err)
		} else {
			dockerHealth.status = "healthy"
		}

		// containerd connectivity
		// todo - explore https://github.com/containerd/containerd/tree/master/pkg/cri
		containerdHealth := containerRuntimeHealth{
			runtimeType: "containerd",
		}
		_, err = containerdCli.ListImages(ctx)
		if err != nil {
			containerdHealth.status = "impaired"
			containerdHealth.message = fmt.Sprintf("containerd health check failed: %+v", err)
		} else {
			containerdHealth.status = "healthy"
		}

		// aggregate the results
		aggregatedHealth := healthReport{[]containerRuntimeHealth{dockerHealth, containerdHealth}}
		fmt.Println(fmt.Sprintf("publishing healthcheck result: %+v", aggregatedHealth))

	}
}

package main

import (
	"context"
	"fmt"
	"github.com/containerd/containerd/namespaces"
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

	//// check init status
	//cmd := "systemctl status ecs | grep PID"
	//fmt.Printf("about to execute bash command  %s\n", cmd)
	//
	//out, err := exec.Command("bash", "-c", cmd).Output()
	//if err != nil {
	//	log.Fatal(err)
	//}
	//fmt.Printf("systemctl output is %s\n", out)

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
		// Because the client calls containerd's gRPC API to interact with the daemon,
		// all API calls require a context with a namespace set.
		containerdCtx := namespaces.WithNamespace(ctx, "moby")
		_, err = containerdCli.ListImages(containerdCtx)
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

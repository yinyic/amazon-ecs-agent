package task

import (
	"encoding/json"
	"fmt"
	apicontainer "github.com/aws/amazon-ecs-agent/agent/api/container"
	apicontainerstatus "github.com/aws/amazon-ecs-agent/agent/api/container/status"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"strconv"
)

type manager struct {
}

func NewServiceConnectManager() ServiceConnectManager {
	return &manager{}
}

// AddNetworkResourceProvisioningDependencyServiceConnectBridge creates one pause container per task container
// including SC container, and add a dependency for SC container to wait for all pause container RESOURCES_PROVISIONED.
//
// SC pause container will use CNI plugin for configuring tproxy, while other pause container(s) will configure ip route
// to send SC traffic to SC container
func (m *manager) AddNetworkResourceProvisioningDependencyBridgeMode(task *Task, cfg *config.Config) error {
	return m.initSCBridgePauseContainers(task, cfg)
}

func (m *manager) AugmentTaskContainerHostConfig(task *Task, container *apicontainer.Container, hostConfig *dockercontainer.HostConfig) error {
	// port mappings
	portMap, err := m.initPortMapping(task, container)
	if err != nil {
		return err
	}
	hostConfig.PortBindings = portMap

	// DNS
	// TODO [SC] move function from engine SC manager

	// bind mounts
	// TODO [SC] move function from engine SC manager

	return nil
}

func (m *manager) AugmentTaskContainerDockerConfig(task *Task, container *apicontainer.Container, dockerConfig *dockercontainer.Config) error {
	// exposed ports
	exposedPorts, err := m.initExposedPorts(task, container)
	if err != nil {
		return err
	}
	dockerConfig.ExposedPorts = exposedPorts

	// ENVs
	// TODO [SC] move function from engine SC manager

	return nil
}


func (m *manager) InitTaskResourcesPostUnmarshal(task *Task) error {
	// container dependencies
	m.initContainerOrdering(task)

	// ephemeral ports
	return m.initServiceConnectEphemeralPorts(task)
}

func (m *manager) initPortMapping(task *Task, container *apicontainer.Container) (nat.PortMap, error) {
	dockerPortMap := nat.PortMap{}
	scContainer := task.GetServiceConnectContainer()
	containerToCheck := container
	if task.IsServiceConnectEnabled() && task.IsNetworkModeBridge() {
		if container.Type == apicontainer.ContainerCNIPause {
			// we will create bindings for task containers (including both customer containers and SC Appnet container)
			// and let them be published by the associated pause container.
			// Note - for SC bridge mode we do not allow customer to specify a host port for their containers. Additionally,
			// When an ephemeral host port is assigned, Appnet will NOT proxy traffic to that port
			taskContainer, err := task.getBridgeModeTaskContainerForPauseContainer(container)
			if err != nil {
				return nil, err
			}
			if taskContainer == scContainer {
				// create bindings for all ingress listener ports
				// no need to create binding for egress listener port as it won't be access from host level or from outside
				for _, ic := range task.ServiceConnectConfig.IngressConfig {
					dockerPort := nat.Port(strconv.Itoa(int(ic.ListenerPort))) + "/tcp"
					hostPort := 0           // default bridge-mode SC experience - host port will be an ephemeral port assigned by docker
					if ic.HostPort != nil { // non-default bridge-mode SC experience - host port specified by customer
						hostPort = int(*ic.HostPort)
					}
					dockerPortMap[dockerPort] = append(dockerPortMap[dockerPort], nat.PortBinding{HostPort: strconv.Itoa(hostPort)})
				}
				return dockerPortMap, nil
			}
			containerToCheck = taskContainer
		} else {
			// If container is neither SC container nor pause container, it's a regular task container. Its port bindings(s)
			// are published by the associated pause container, and we leave the map empty here (docker would actually complain
			// otherwise).
			return dockerPortMap, nil
		}
	}

	for _, portBinding := range containerToCheck.Ports {
		dockerPort := nat.Port(strconv.Itoa(int(portBinding.ContainerPort)) + "/" + portBinding.Protocol.String())
		dockerPortMap[dockerPort] = append(dockerPortMap[dockerPort], nat.PortBinding{HostPort: strconv.Itoa(int(portBinding.HostPort))})
	}
	return dockerPortMap, nil
}

func (m *manager) initExposedPorts(task *Task, container *apicontainer.Container) (dockerExposedPorts nat.PortSet, err error) {
	containerToCheck := container
	scContainer := task.GetServiceConnectContainer()
	dockerExposedPorts = make(map[nat.Port]struct{})

	if task.IsNetworkModeBridge() {
		if container.Type == apicontainer.ContainerCNIPause {
			// find the task container associated with this particular pause container, and let pause container
			// expose the application container port
			containerToCheck, err = task.getBridgeModeTaskContainerForPauseContainer(container)
			if err != nil {
				return nil, err
			}
			// if the associated task container is SC container, expose all its ingress and egress listener ports if present
			if containerToCheck == scContainer {
				for _, ic := range task.ServiceConnectConfig.IngressConfig {
					dockerPort := nat.Port(strconv.Itoa(int(ic.ListenerPort))) + "/tcp"
					dockerExposedPorts[dockerPort] = struct{}{}
				}
				ec := task.ServiceConnectConfig.EgressConfig
				if ec.ListenerName != "" { // it's possible that task does not have an egress listener
					dockerPort := nat.Port(strconv.Itoa(int(ec.ListenerPort))) + "/tcp"
					dockerExposedPorts[dockerPort] = struct{}{}
				}
				return dockerExposedPorts, nil
			}
		} else {
			// This is a task container which is launched with "--network container:$pause_container_id"
			// In such case we don't expose any ports (docker won't allow anyway) because they are exposed by their
			// pause container instead.
			return dockerExposedPorts, nil
		}
	}
	for _, portBinding := range containerToCheck.Ports {
		dockerPort := nat.Port(strconv.Itoa(int(portBinding.ContainerPort)) + "/" + portBinding.Protocol.String())
		dockerExposedPorts[dockerPort] = struct{}{}
	}
	return dockerExposedPorts, nil
}

func (m *manager) initContainerOrdering(task *Task) {
	scContainer := task.GetServiceConnectContainer()

	for _, container := range task.Containers {
		if container.IsInternal() || container == scContainer {
			continue
		}
		container.AddContainerDependency(scContainer.Name, ContainerOrderingHealthyCondition)
		scContainer.BuildContainerDependency(container.Name, apicontainerstatus.ContainerStopped, apicontainerstatus.ContainerStopped)
	}
}

func (m *manager) initServiceConnectEphemeralPorts(task *Task) error {
	var utilizedPorts []uint16
	// First determine how many ephemeral ports we need
	var numEphemeralPortsNeeded int
	for _, ic := range task.ServiceConnectConfig.IngressConfig {
		if ic.ListenerPort == 0 { // This means listener port was not sent to us by ACS, signaling the port needs to be ephemeral
			numEphemeralPortsNeeded++
		} else {
			utilizedPorts = append(utilizedPorts, ic.ListenerPort)
		}
	}

	// Presently, SC egress port is always ephemeral, but adding this for future-proofing
	if task.ServiceConnectConfig.EgressConfig.ListenerPort == 0 {
		numEphemeralPortsNeeded++
	} else {
		utilizedPorts = append(utilizedPorts, task.ServiceConnectConfig.EgressConfig.ListenerPort)
	}

	// Get all exposed ports in the task so that the ephemeral port generator doesn't take those into account in order
	// to avoid port conflicts.
	for _, c := range task.Containers {
		for _, p := range c.Ports {
			utilizedPorts = append(utilizedPorts, p.ContainerPort)
		}
	}

	ephemeralPorts, err := utils.GenerateEphemeralPortNumbers(numEphemeralPortsNeeded, utilizedPorts)
	if err != nil {
		return fmt.Errorf("error initializing ports for Service Connect: %w", err)
	}

	// Assign ephemeral ports
	portMapping := make(map[string]uint16)
	var curEphemeralIndex int
	for i, ic := range task.ServiceConnectConfig.IngressConfig {
		if ic.ListenerPort == 0 {
			portMapping[ic.ListenerName] = ephemeralPorts[curEphemeralIndex]
			task.ServiceConnectConfig.IngressConfig[i].ListenerPort = ephemeralPorts[curEphemeralIndex]
			curEphemeralIndex++
		}
	}

	if task.ServiceConnectConfig.EgressConfig.ListenerPort == 0 {
		portMapping[task.ServiceConnectConfig.EgressConfig.ListenerName] = ephemeralPorts[curEphemeralIndex]
		task.ServiceConnectConfig.EgressConfig.ListenerPort = ephemeralPorts[curEphemeralIndex]
	}

	// Add the APPNET_LISTENER_PORT_MAPPING env var for listeners that require it
	// TODO [SC]: Move to where we initialize container IP mapping ENV
	envVars := make(map[string]string)
	portMappingJson, err := json.Marshal(portMapping)
	if err != nil {
		return fmt.Errorf("error injecting required env vars to Service Connect container: %w", err)
	}
	envVars[serviceConnectListenerPortMappingEnvVar] = string(portMappingJson)
	task.GetServiceConnectContainer().MergeEnvironmentVariables(envVars)
	return nil
}

func (m *manager) initSCBridgePauseContainers(task *Task, cfg *config.Config) error {
	scContainer := task.GetServiceConnectContainer()
	var scPauseContainer *apicontainer.Container
	for _, container := range task.Containers {
		if container.IsInternal() {
			continue
		}
		pauseContainer := apicontainer.NewContainerWithSteadyState(apicontainerstatus.ContainerResourcesProvisioned)
		pauseContainer.TransitionDependenciesMap = make(map[apicontainerstatus.ContainerStatus]apicontainer.TransitionDependencySet)
		// The pause container name is used internally by task engine but still needs to be unique for every task,
		// hence we are appending the corresponding application container name (which must already be unique within the task)
		pauseContainer.Name = fmt.Sprintf("%s-%s", NetworkPauseContainerName, container.Name)
		pauseContainer.Image = fmt.Sprintf("%s:%s", cfg.PauseContainerImageName, cfg.PauseContainerTag)
		pauseContainer.Essential = true
		pauseContainer.Type = apicontainer.ContainerCNIPause

		task.Containers = append(task.Containers, pauseContainer)
		// SC container CREATED will depend on ALL pause containers RESOURCES_PROVISIONED
		scContainer.BuildContainerDependency(pauseContainer.Name, apicontainerstatus.ContainerResourcesProvisioned, apicontainerstatus.ContainerCreated)
		pauseContainer.BuildContainerDependency(scContainer.Name, apicontainerstatus.ContainerStopped, apicontainerstatus.ContainerStopped)
		if container == scContainer {
			scPauseContainer = pauseContainer
		}
	}

	// All other task pause container RESOURCES_PROVISIONED depends on SC pause container RUNNING because task pause container
	// CNI plugin invocation needs the IP of SC pause container (to send SC traffic to)
	for _, container := range task.Containers {
		if container.Type != apicontainer.ContainerCNIPause || container == scPauseContainer {
			continue
		}
		container.BuildContainerDependency(scPauseContainer.Name, apicontainerstatus.ContainerRunning, apicontainerstatus.ContainerResourcesProvisioned)
	}
	return nil
}

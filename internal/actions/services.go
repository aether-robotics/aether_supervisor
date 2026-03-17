package actions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerFilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/sirupsen/logrus"

	composepkg "github.com/aether-robotics/aether_supervisor/pkg/compose"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

const (
	serviceStopTimeout = 10 * time.Second
	defaultShell       = "/bin/sh"
)

var (
	errAppNameRequired       = errors.New("app name is required")
	errNoMatchingContainers  = errors.New("no matching containers found")
	errUnsupportedVolumeType = errors.New("unsupported volume type")
)

// ServicesDeploySummary summarizes a deployment request.
type ServicesDeploySummary struct {
	Application string
	Services    []string
	Created     int
}

// ServicesActionSummary summarizes a lifecycle action request.
type ServicesActionSummary struct {
	Name     string
	Service  string
	Affected int
}

// DeployServices creates containers for the provided app spec.
func DeployServices(ctx context.Context, spec types.AppSpec) (*ServicesDeploySummary, error) {
	if spec.Name == "" {
		return nil, errAppNameRequired
	}

	api, err := newDockerAPIClient()
	if err != nil {
		return nil, err
	}
	defer api.Close()

	networkNames, err := ensureNetworks(ctx, api, spec)
	if err != nil {
		return nil, err
	}

	serviceNames, err := orderedServiceNames(spec.Services)
	if err != nil {
		return nil, err
	}

	created := make([]string, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		serviceSpec := spec.Services[serviceName]

		if err := pullImage(ctx, api, serviceSpec.Image); err != nil {
			return nil, err
		}

		cfg, hostCfg, netCfg, err := buildServiceConfig(spec, serviceName, serviceSpec, networkNames)
		if err != nil {
			return nil, err
		}

		containerName := resolveContainerName(spec.Name, serviceName, serviceSpec)
		if _, err := api.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName); err != nil {
			return nil, fmt.Errorf("create container %s: %w", containerName, err)
		}

		if err := api.ContainerStart(ctx, containerName, container.StartOptions{}); err != nil {
			return nil, fmt.Errorf("start container %s: %w", containerName, err)
		}

		created = append(created, serviceName)
	}

	return &ServicesDeploySummary{
		Application: spec.Name,
		Services:    created,
		Created:     len(created),
	}, nil
}

// StopServices stops all matched containers for an app or a specific service.
func StopServices(ctx context.Context, target types.ServiceTarget) (*ServicesActionSummary, error) {
	return actOnServices(ctx, target, func(ctx context.Context, api dockerClient.APIClient, id string) error {
		timeout := int(serviceStopTimeout.Seconds())
		return api.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
	})
}

// StartServices starts matched containers for a specific service.
func StartServices(ctx context.Context, target types.ServiceTarget) (*ServicesActionSummary, error) {
	return actOnServices(ctx, target, func(ctx context.Context, api dockerClient.APIClient, id string) error {
		return api.ContainerStart(ctx, id, container.StartOptions{})
	})
}

// RestartServices restarts matched containers for a specific service.
func RestartServices(ctx context.Context, target types.ServiceTarget) (*ServicesActionSummary, error) {
	return actOnServices(ctx, target, func(ctx context.Context, api dockerClient.APIClient, id string) error {
		timeout := int(serviceStopTimeout.Seconds())
		return api.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
	})
}

// RemoveServices removes matched containers for an app or a specific service.
func RemoveServices(ctx context.Context, target types.ServiceTarget) (*ServicesActionSummary, error) {
	return actOnServices(ctx, target, func(ctx context.Context, api dockerClient.APIClient, id string) error {
		timeout := int(serviceStopTimeout.Seconds())
		if err := api.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil && !dockerClient.IsErrNotFound(err) {
			// Ignore stop failures for already-stopped containers and proceed with removal.
			logrus.WithError(err).WithField("container_id", id).Debug("Container stop before remove failed")
		}

		return api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	})
}

func actOnServices(
	ctx context.Context,
	target types.ServiceTarget,
	action func(context.Context, dockerClient.APIClient, string) error,
) (*ServicesActionSummary, error) {
	if target.Name == "" {
		return nil, errAppNameRequired
	}

	api, err := newDockerAPIClient()
	if err != nil {
		return nil, err
	}
	defer api.Close()

	containers, err := listTargetContainers(ctx, api, target)
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, errNoMatchingContainers
	}

	for _, c := range containers {
		if err := action(ctx, api, c.ID); err != nil {
			return nil, fmt.Errorf("service action failed for %s: %w", c.ID[:12], err)
		}
	}

	return &ServicesActionSummary{
		Name:     target.Name,
		Service:  target.Service,
		Affected: len(containers),
	}, nil
}

func newDockerAPIClient() (dockerClient.APIClient, error) {
	return dockerClient.NewClientWithOpts(
		dockerClient.FromEnv,
		dockerClient.WithAPIVersionNegotiation(),
	)
}

func listTargetContainers(
	ctx context.Context,
	api dockerClient.APIClient,
	target types.ServiceTarget,
) ([]container.Summary, error) {
	args := dockerFilters.NewArgs()
	args.Add("label", composepkg.ComposeProjectLabel+"="+target.Name)
	if target.Service != "" {
		args.Add("label", composepkg.ComposeServiceLabel+"="+target.Service)
	}

	containers, err := api.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("list target containers: %w", err)
	}

	sort.Slice(containers, func(i, j int) bool {
		return firstContainerName(containers[i]) < firstContainerName(containers[j])
	})

	return containers, nil
}

func ensureNetworks(
	ctx context.Context,
	api dockerClient.APIClient,
	spec types.AppSpec,
) (map[string]string, error) {
	networkNames := make(map[string]string, len(spec.Networks))

	for key, netSpec := range spec.Networks {
		actualName := resolveNetworkName(spec.Name, key, netSpec)
		networkNames[key] = actualName

		if netSpec.External {
			continue
		}

		args := dockerFilters.NewArgs()
		args.Add("name", actualName)
		existing, err := api.NetworkList(ctx, network.ListOptions{Filters: args})
		if err != nil {
			return nil, fmt.Errorf("list networks: %w", err)
		}
		if len(existing) > 0 {
			continue
		}

		labels := labelsToMap(netSpec.Labels)
		resp, err := api.NetworkCreate(ctx, actualName, network.CreateOptions{
			Driver:     netSpec.Driver,
			Internal:   netSpec.Internal,
			Attachable: netSpec.Attachable,
			Labels:     labels,
			Options:    netSpec.DriverOpts,
		})
		if err != nil {
			return nil, fmt.Errorf("create network %s: %w", actualName, err)
		}

		logrus.WithFields(logrus.Fields{
			"network":    actualName,
			"network_id": resp.ID,
		}).Debug("Created app network")
	}

	return networkNames, nil
}

func buildServiceConfig(
	appSpec types.AppSpec,
	serviceName string,
	serviceSpec types.ServiceSpec,
	networkNames map[string]string,
) (*container.Config, *container.HostConfig, *network.NetworkingConfig, error) {
	envValues, err := envValues(serviceSpec)
	if err != nil {
		return nil, nil, nil, err
	}

	exposedPorts, portBindings, err := portConfig(serviceSpec)
	if err != nil {
		return nil, nil, nil, err
	}

	mounts, err := mountConfig(serviceSpec, appSpec)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := &container.Config{
		Image:        serviceSpec.Image,
		Env:          envValues,
		Cmd:          commandToSlice(serviceSpec.Command),
		Entrypoint:   commandToSlice(serviceSpec.Entrypoint),
		Tty:          serviceSpec.TTY,
		OpenStdin:    serviceSpec.StdinOpen,
		WorkingDir:   serviceSpec.WorkingDir,
		User:         serviceSpec.User,
		ExposedPorts: exposedPorts,
		Labels:       serviceLabels(appSpec, serviceName, serviceSpec),
		Healthcheck:  healthcheckConfig(serviceSpec.Healthcheck),
	}

	hostCfg := &container.HostConfig{
		Privileged:    serviceSpec.Privileged,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(serviceSpec.Restart)},
		NetworkMode:   container.NetworkMode(serviceSpec.NetworkMode),
		Binds:         nil,
		Mounts:        mounts,
		PortBindings:  portBindings,
		ExtraHosts:    serviceSpec.ExtraHosts.Values,
		DNS:           serviceSpec.DNS.Values,
		CapAdd:        strSliceToCapAdd(serviceSpec.CapAdd.Values),
		CapDrop:       strSliceToCapDrop(serviceSpec.CapDrop.Values),
		Resources: container.Resources{
			Devices: devicesToMappings(serviceSpec.Devices.Values),
		},
	}

	netCfg := &network.NetworkingConfig{}
	if hostCfg.NetworkMode == "" {
		endpoints := buildEndpointConfig(appSpec, serviceName, serviceSpec, networkNames)
		if len(endpoints) > 0 {
			netCfg.EndpointsConfig = endpoints
		}
	}

	return cfg, hostCfg, netCfg, nil
}

func serviceLabels(appSpec types.AppSpec, serviceName string, serviceSpec types.ServiceSpec) map[string]string {
	labels := labelsToMap(serviceSpec.Labels)
	if labels == nil {
		labels = map[string]string{}
	}

	labels[composepkg.ComposeProjectLabel] = appSpec.Name
	labels[composepkg.ComposeServiceLabel] = serviceName
	labels[composepkg.ComposeContainerNumber] = "1"

	if dependsOn := dependsOnValue(serviceSpec.DependsOn); dependsOn != "" {
		labels[composepkg.ComposeDependsOnLabel] = dependsOn
	}

	return labels
}

func dependsOnValue(dependsOn types.DependsOnSpec) string {
	if len(dependsOn.Values) > 0 {
		payload := map[string]map[string]any{}
		for name, dep := range dependsOn.Values {
			entry := map[string]any{}
			if dep.Condition != "" {
				entry["condition"] = dep.Condition
			}
			if dep.Restart {
				entry["restart"] = dep.Restart
			}
			if dep.Required != nil {
				entry["required"] = *dep.Required
			}
			payload[name] = entry
		}

		encoded, err := json.Marshal(payload)
		if err == nil {
			return string(encoded)
		}
	}

	if len(dependsOn.Names) > 0 {
		return strings.Join(dependsOn.Names, ",")
	}

	return ""
}

func labelsToMap(value types.MappingOrList) map[string]string {
	if len(value.Mapping) > 0 {
		result := make(map[string]string, len(value.Mapping))
		for key, val := range value.Mapping {
			if val == nil {
				result[key] = ""
				continue
			}
			result[key] = *val
		}

		return result
	}

	if len(value.List) > 0 {
		result := make(map[string]string, len(value.List))
		for _, entry := range value.List {
			if idx := strings.Index(entry, "="); idx >= 0 {
				result[entry[:idx]] = entry[idx+1:]
			} else {
				result[entry] = ""
			}
		}

		return result
	}

	return nil
}

func envValues(serviceSpec types.ServiceSpec) ([]string, error) {
	values := make([]string, 0)

	for _, envFile := range serviceSpec.EnvFile.Values {
		fileValues, err := readEnvFile(envFile)
		if err != nil {
			return nil, err
		}
		values = append(values, fileValues...)
	}

	if len(serviceSpec.Environment.Mapping) > 0 {
		keys := make([]string, 0, len(serviceSpec.Environment.Mapping))
		for key := range serviceSpec.Environment.Mapping {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := serviceSpec.Environment.Mapping[key]
			if val == nil {
				values = append(values, key)
			} else {
				values = append(values, key+"="+*val)
			}
		}

		return values, nil
	}

	values = append(values, serviceSpec.Environment.List...)

	return values, nil
}

func readEnvFile(path string) ([]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open env_file %s: %w", path, err)
	}
	defer file.Close()

	values := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		values = append(values, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env_file %s: %w", path, err)
	}

	return values, nil
}

func commandToSlice(command types.CommandValue) []string {
	if command.Shell != nil {
		return []string{defaultShell, "-c", *command.Shell}
	}

	if len(command.Exec) == 0 {
		return nil
	}

	return append([]string(nil), command.Exec...)
}

func healthcheckConfig(spec *types.HealthcheckSpec) *container.HealthConfig {
	if spec == nil {
		return nil
	}

	if spec.Disable {
		return &container.HealthConfig{Test: []string{"NONE"}}
	}

	test := []string(nil)
	if spec.Test.Shell != nil {
		test = []string{"CMD-SHELL", *spec.Test.Shell}
	} else if len(spec.Test.Exec) > 0 {
		test = append([]string(nil), spec.Test.Exec...)
	}

	return &container.HealthConfig{
		Test:          test,
		Interval:      parseDuration(spec.Interval),
		Timeout:       parseDuration(spec.Timeout),
		StartPeriod:   parseDuration(spec.StartPeriod),
		StartInterval: parseDuration(spec.StartIntvl),
		Retries:       int(spec.Retries),
	}
}

func parseDuration(raw string) time.Duration {
	if raw == "" {
		return 0
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}

	return duration
}

func mountConfig(serviceSpec types.ServiceSpec, appSpec types.AppSpec) ([]mount.Mount, error) {
	result := make([]mount.Mount, 0, len(serviceSpec.Volumes))
	for _, value := range serviceSpec.Volumes {
		if value.Config != nil {
			m, err := longMount(*value.Config, appSpec)
			if err != nil {
				return nil, err
			}
			result = append(result, m)
			continue
		}

		m, err := shortMount(value.Short, appSpec)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}

	return result, nil
}

func shortMount(raw string, appSpec types.AppSpec) (mount.Mount, error) {
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 1:
		return mount.Mount{Type: mount.TypeVolume, Target: parts[0]}, nil
	case 2, 3:
		source := parts[0]
		target := parts[1]
		readOnly := len(parts) == 3 && strings.Contains(parts[2], "ro")
		mountType, resolvedSource := resolveShortMountSource(appSpec, source)
		return mount.Mount{
			Type:     mountType,
			Source:   resolvedSource,
			Target:   target,
			ReadOnly: readOnly,
		}, nil
	default:
		return mount.Mount{}, fmt.Errorf("invalid volume spec: %s", raw)
	}
}

func longMount(spec types.ServiceVolumeConfig, appSpec types.AppSpec) (mount.Mount, error) {
	mountType := mount.Type(spec.Type)
	if mountType == "" {
		mountType = mount.TypeVolume
	}

	source := spec.Source
	if mountType == mount.TypeVolume {
		if volumeSpec, ok := appSpec.Volumes[source]; ok && !volumeSpec.External {
			source = resolveVolumeName(appSpec.Name, source, volumeSpec)
		}
	}

	switch mountType {
	case mount.TypeBind, mount.TypeVolume, mount.TypeTmpfs:
	default:
		return mount.Mount{}, fmt.Errorf("%w: %s", errUnsupportedVolumeType, spec.Type)
	}

	m := mount.Mount{
		Type:     mountType,
		Source:   source,
		Target:   spec.Target,
		ReadOnly: spec.ReadOnly,
	}
	if spec.Bind != nil {
		m.BindOptions = &mount.BindOptions{
			Propagation:      mount.Propagation(spec.Bind.Propagation),
			CreateMountpoint: spec.Bind.CreateHostPath,
		}
	}
	if spec.Volume != nil {
		m.VolumeOptions = &mount.VolumeOptions{
			NoCopy:  spec.Volume.NoCopy,
			Subpath: spec.Volume.SubPath,
		}
	}
	if spec.Tmpfs != nil {
		m.TmpfsOptions = &mount.TmpfsOptions{
			SizeBytes: spec.Tmpfs.Size,
			Mode:      os.FileMode(spec.Tmpfs.Mode),
		}
	}

	return m, nil
}

func resolveShortMountSource(appSpec types.AppSpec, source string) (mount.Type, string) {
	if volumeSpec, ok := appSpec.Volumes[source]; ok {
		if volumeSpec.External {
			return mount.TypeVolume, firstNonEmpty(volumeSpec.Name, source)
		}
		return mount.TypeVolume, resolveVolumeName(appSpec.Name, source, volumeSpec)
	}

	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") {
		return mount.TypeBind, source
	}

	return mount.TypeVolume, source
}

func portConfig(serviceSpec types.ServiceSpec) (nat.PortSet, nat.PortMap, error) {
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}

	for _, value := range serviceSpec.Ports {
		if value.Config == nil {
			ports, bindings, err := nat.ParsePortSpecs([]string{value.Short})
			if err != nil {
				return nil, nil, err
			}
			for port := range ports {
				exposedPorts[port] = struct{}{}
			}
			for port, binding := range bindings {
				portBindings[port] = append(portBindings[port], binding...)
			}
			continue
		}

		cfg := value.Config
		protocol := firstNonEmpty(cfg.Protocol, "tcp")
		port, err := nat.NewPort(protocol, fmt.Sprintf("%d", cfg.Target))
		if err != nil {
			return nil, nil, err
		}
		exposedPorts[port] = struct{}{}
		if cfg.Published != "" || cfg.HostIP != "" {
			portBindings[port] = append(portBindings[port], nat.PortBinding{
				HostIP:   cfg.HostIP,
				HostPort: cfg.Published,
			})
		}
	}

	return exposedPorts, portBindings, nil
}

func buildEndpointConfig(
	appSpec types.AppSpec,
	serviceName string,
	serviceSpec types.ServiceSpec,
	networkNames map[string]string,
) map[string]*network.EndpointSettings {
	result := map[string]*network.EndpointSettings{}

	if len(serviceSpec.Networks.Values) > 0 {
		for key, attachment := range serviceSpec.Networks.Values {
			name := resolveServiceNetworkName(appSpec, key, networkNames)
			result[name] = &network.EndpointSettings{
				Aliases:           attachment.Aliases,
				IPAddress:         attachment.IPv4Address,
				GlobalIPv6Address: attachment.IPv6Address,
				IPAMConfig: &network.EndpointIPAMConfig{
					IPv4Address: attachment.IPv4Address,
					IPv6Address: attachment.IPv6Address,
				},
			}
			if len(result[name].Aliases) == 0 {
				result[name].Aliases = []string{serviceName}
			}
		}
		return result
	}

	for _, key := range serviceSpec.Networks.Names {
		name := resolveServiceNetworkName(appSpec, key, networkNames)
		result[name] = &network.EndpointSettings{
			Aliases: []string{serviceName},
		}
	}

	return result
}

func resolveServiceNetworkName(appSpec types.AppSpec, key string, networkNames map[string]string) string {
	if name, ok := networkNames[key]; ok {
		return name
	}
	if netSpec, ok := appSpec.Networks[key]; ok {
		return resolveNetworkName(appSpec.Name, key, netSpec)
	}
	return key
}

func resolveNetworkName(appName, key string, netSpec types.NetworkSpec) string {
	if netSpec.Name != "" {
		return netSpec.Name
	}
	return appName + "_" + key
}

func resolveVolumeName(appName, key string, volumeSpec types.VolumeSpec) string {
	if volumeSpec.Name != "" {
		return volumeSpec.Name
	}
	return appName + "_" + key
}

func resolveContainerName(appName, serviceName string, serviceSpec types.ServiceSpec) string {
	if serviceSpec.ContainerName != "" {
		return serviceSpec.ContainerName
	}
	return fmt.Sprintf("%s-%s-1", appName, serviceName)
}

func orderedServiceNames(services map[string]types.ServiceSpec) ([]string, error) {
	ordered := []string{}
	visited := map[string]int{}

	var visit func(string) error
	visit = func(name string) error {
		switch visited[name] {
		case 1:
			return fmt.Errorf("circular depends_on at service %s", name)
		case 2:
			return nil
		}
		visited[name] = 1
		spec, ok := services[name]
		if !ok {
			return fmt.Errorf("unknown service dependency %s", name)
		}
		for _, dep := range serviceDependencies(spec) {
			if _, ok := services[dep]; !ok {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[name] = 2
		ordered = append(ordered, name)
		return nil
	}

	keys := make([]string, 0, len(services))
	for key := range services {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := visit(key); err != nil {
			return nil, err
		}
	}

	return ordered, nil
}

func serviceDependencies(spec types.ServiceSpec) []string {
	if len(spec.DependsOn.Values) > 0 {
		result := make([]string, 0, len(spec.DependsOn.Values))
		for name := range spec.DependsOn.Values {
			result = append(result, name)
		}
		sort.Strings(result)
		return result
	}
	return append([]string(nil), spec.DependsOn.Names...)
}

func pullImage(ctx context.Context, api dockerClient.APIClient, imageName string) error {
	if imageName == "" {
		return fmt.Errorf("service image is required")
	}

	reader, err := api.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func devicesToMappings(values []string) []container.DeviceMapping {
	result := make([]container.DeviceMapping, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		device := container.DeviceMapping{}
		switch len(parts) {
		case 1:
			device.PathOnHost = parts[0]
			device.PathInContainer = parts[0]
		case 2:
			device.PathOnHost = parts[0]
			device.PathInContainer = parts[1]
		default:
			device.PathOnHost = parts[0]
			device.PathInContainer = parts[1]
			device.CgroupPermissions = parts[2]
		}
		result = append(result, device)
	}
	return result
}

func strSliceToCapAdd(values []string) []string  { return append([]string(nil), values...) }
func strSliceToCapDrop(values []string) []string { return append([]string(nil), values...) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstContainerName(c container.Summary) string {
	if len(c.Names) == 0 {
		return c.ID
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

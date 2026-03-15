package types

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// AppSpec defines a Compose-like application document.
type AppSpec struct {
	Name     string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Services map[string]ServiceSpec `json:"services" yaml:"services"`
	Networks map[string]NetworkSpec `json:"networks,omitempty" yaml:"networks,omitempty"`
	Volumes  map[string]VolumeSpec  `json:"volumes,omitempty" yaml:"volumes,omitempty"`
}

// ServiceSpec defines a Compose-like service document for the Tier 1 fields.
type ServiceSpec struct {
	ContainerName string                 `json:"container_name,omitempty" yaml:"container_name,omitempty"`
	Image         string                 `json:"image,omitempty" yaml:"image,omitempty"`
	Command       CommandValue           `json:"command,omitempty" yaml:"command,omitempty"`
	Entrypoint    CommandValue           `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Environment   MappingOrList          `json:"environment,omitempty" yaml:"environment,omitempty"`
	EnvFile       StringList             `json:"env_file,omitempty" yaml:"env_file,omitempty"`
	Volumes       []VolumeValue          `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	Ports         []PortValue            `json:"ports,omitempty" yaml:"ports,omitempty"`
	Restart       string                 `json:"restart,omitempty" yaml:"restart,omitempty"`
	NetworkMode   string                 `json:"network_mode,omitempty" yaml:"network_mode,omitempty"`
	Networks      ServiceNetworks        `json:"networks,omitempty" yaml:"networks,omitempty"`
	DependsOn     DependsOnSpec          `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Privileged    bool                   `json:"privileged,omitempty" yaml:"privileged,omitempty"`
	TTY           bool                   `json:"tty,omitempty" yaml:"tty,omitempty"`
	StdinOpen     bool                   `json:"stdin_open,omitempty" yaml:"stdin_open,omitempty"`
	WorkingDir    string                 `json:"working_dir,omitempty" yaml:"working_dir,omitempty"`
	User          string                 `json:"user,omitempty" yaml:"user,omitempty"`
	Devices       StringList             `json:"devices,omitempty" yaml:"devices,omitempty"`
	CapAdd        StringList             `json:"cap_add,omitempty" yaml:"cap_add,omitempty"`
	CapDrop       StringList             `json:"cap_drop,omitempty" yaml:"cap_drop,omitempty"`
	DNS           StringList             `json:"dns,omitempty" yaml:"dns,omitempty"`
	ExtraHosts    StringList             `json:"extra_hosts,omitempty" yaml:"extra_hosts,omitempty"`
	Labels        MappingOrList          `json:"labels,omitempty" yaml:"labels,omitempty"`
	Healthcheck   *HealthcheckSpec       `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`
	Extensions    map[string]interface{} `json:"-" yaml:",inline"`
}

// CommandValue stores either shell-form or exec-form commands.
type CommandValue struct {
	Shell *string  `json:"-" yaml:"-"`
	Exec  []string `json:"-" yaml:"-"`
}

// IsZero reports whether the command is unset.
func (c CommandValue) IsZero() bool {
	return c.Shell == nil && len(c.Exec) == 0
}

// MappingOrList stores either a key/value mapping or a list representation.
type MappingOrList struct {
	Mapping map[string]*string `json:"-" yaml:"-"`
	List    []string           `json:"-" yaml:"-"`
}

// IsZero reports whether the value is unset.
func (m MappingOrList) IsZero() bool {
	return len(m.Mapping) == 0 && len(m.List) == 0
}

// StringList stores either a single string or a list of strings.
type StringList struct {
	Values []string `json:"-" yaml:"-"`
}

// IsZero reports whether the list is unset.
func (s StringList) IsZero() bool {
	return len(s.Values) == 0
}

// PortValue stores either short-form or long-form port syntax.
type PortValue struct {
	Short  string             `json:"-" yaml:"-"`
	Config *ServicePortConfig `json:"-" yaml:"-"`
}

// VolumeValue stores either short-form or long-form volume syntax.
type VolumeValue struct {
	Short  string               `json:"-" yaml:"-"`
	Config *ServiceVolumeConfig `json:"-" yaml:"-"`
}

// ServiceNetworks stores either list or map network attachment syntax.
type ServiceNetworks struct {
	Names  []string                            `json:"-" yaml:"-"`
	Values map[string]ServiceNetworkAttachment `json:"-" yaml:"-"`
}

// IsZero reports whether the network attachment set is unset.
func (s ServiceNetworks) IsZero() bool {
	return len(s.Names) == 0 && len(s.Values) == 0
}

// DependsOnSpec stores either list or map dependency syntax.
type DependsOnSpec struct {
	Names  []string                     `json:"-" yaml:"-"`
	Values map[string]ServiceDependency `json:"-" yaml:"-"`
}

// IsZero reports whether the dependency set is unset.
func (d DependsOnSpec) IsZero() bool {
	return len(d.Names) == 0 && len(d.Values) == 0
}

// ServicePortConfig defines long-form Compose port syntax.
type ServicePortConfig struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Mode        string `json:"mode,omitempty" yaml:"mode,omitempty"`
	HostIP      string `json:"host_ip,omitempty" yaml:"host_ip,omitempty"`
	Target      uint32 `json:"target,omitempty" yaml:"target,omitempty"`
	Published   string `json:"published,omitempty" yaml:"published,omitempty"`
	Protocol    string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	AppProtocol string `json:"app_protocol,omitempty" yaml:"app_protocol,omitempty"`
}

// ServiceVolumeConfig defines long-form Compose volume syntax.
type ServiceVolumeConfig struct {
	Type     string               `json:"type,omitempty" yaml:"type,omitempty"`
	Source   string               `json:"source,omitempty" yaml:"source,omitempty"`
	Target   string               `json:"target,omitempty" yaml:"target,omitempty"`
	ReadOnly bool                 `json:"read_only,omitempty" yaml:"read_only,omitempty"`
	Bind     *ServiceBindOptions  `json:"bind,omitempty" yaml:"bind,omitempty"`
	Volume   *NamedVolumeOptions  `json:"volume,omitempty" yaml:"volume,omitempty"`
	Tmpfs    *ServiceTmpfsOptions `json:"tmpfs,omitempty" yaml:"tmpfs,omitempty"`
}

// ServiceBindOptions defines long-form bind mount options.
type ServiceBindOptions struct {
	CreateHostPath bool   `json:"create_host_path,omitempty" yaml:"create_host_path,omitempty"`
	Propagation    string `json:"propagation,omitempty" yaml:"propagation,omitempty"`
	Selinux        string `json:"selinux,omitempty" yaml:"selinux,omitempty"`
}

// NamedVolumeOptions defines long-form named volume options.
type NamedVolumeOptions struct {
	NoCopy  bool   `json:"nocopy,omitempty" yaml:"nocopy,omitempty"`
	SubPath string `json:"subpath,omitempty" yaml:"subpath,omitempty"`
}

// ServiceTmpfsOptions defines long-form tmpfs options.
type ServiceTmpfsOptions struct {
	Mode uint32 `json:"mode,omitempty" yaml:"mode,omitempty"`
	Size int64  `json:"size,omitempty" yaml:"size,omitempty"`
}

// ServiceNetworkAttachment defines per-service network attachment options.
type ServiceNetworkAttachment struct {
	Aliases      []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	IPv4Address  string   `json:"ipv4_address,omitempty" yaml:"ipv4_address,omitempty"`
	IPv6Address  string   `json:"ipv6_address,omitempty" yaml:"ipv6_address,omitempty"`
	LinkLocalIPs []string `json:"link_local_ips,omitempty" yaml:"link_local_ips,omitempty"`
	Priority     int      `json:"priority,omitempty" yaml:"priority,omitempty"`
}

// ServiceDependency defines long-form depends_on options.
type ServiceDependency struct {
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
	Restart   bool   `json:"restart,omitempty" yaml:"restart,omitempty"`
	Required  *bool  `json:"required,omitempty" yaml:"required,omitempty"`
}

// HealthcheckSpec defines a Compose-compatible healthcheck subset.
type HealthcheckSpec struct {
	Test        CommandValue `json:"test,omitempty" yaml:"test,omitempty"`
	Interval    string       `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout     string       `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	StartPeriod string       `json:"start_period,omitempty" yaml:"start_period,omitempty"`
	StartIntvl  string       `json:"start_interval,omitempty" yaml:"start_interval,omitempty"`
	Retries     uint         `json:"retries,omitempty" yaml:"retries,omitempty"`
	Disable     bool         `json:"disable,omitempty" yaml:"disable,omitempty"`
}

// NetworkSpec defines a top-level network entry.
type NetworkSpec struct {
	Name       string            `json:"name,omitempty" yaml:"name,omitempty"`
	Driver     string            `json:"driver,omitempty" yaml:"driver,omitempty"`
	External   bool              `json:"external,omitempty" yaml:"external,omitempty"`
	Internal   bool              `json:"internal,omitempty" yaml:"internal,omitempty"`
	Attachable bool              `json:"attachable,omitempty" yaml:"attachable,omitempty"`
	Labels     MappingOrList     `json:"labels,omitempty" yaml:"labels,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty" yaml:"driver_opts,omitempty"`
}

// VolumeSpec defines a top-level volume entry.
type VolumeSpec struct {
	Name       string            `json:"name,omitempty" yaml:"name,omitempty"`
	Driver     string            `json:"driver,omitempty" yaml:"driver,omitempty"`
	External   bool              `json:"external,omitempty" yaml:"external,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty" yaml:"driver_opts,omitempty"`
	Labels     MappingOrList     `json:"labels,omitempty" yaml:"labels,omitempty"`
}

func (c *CommandValue) UnmarshalJSON(data []byte) error {
	return decodeCommandValue(data, json.Unmarshal, c)
}

func (c *CommandValue) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, c.decodeFromRaw)
}

func (c CommandValue) MarshalJSON() ([]byte, error) {
	if c.Shell != nil {
		return json.Marshal(*c.Shell)
	}

	return json.Marshal(c.Exec)
}

func (c *CommandValue) decodeFromRaw(raw interface{}) error {
	c.Shell = nil
	c.Exec = nil

	switch v := raw.(type) {
	case string:
		c.Shell = &v
		return nil
	case []interface{}:
		values, err := toStringSlice(v)
		if err != nil {
			return err
		}
		c.Exec = values
		return nil
	default:
		return fmt.Errorf("expected string or string list, got %T", raw)
	}
}

func (m *MappingOrList) UnmarshalJSON(data []byte) error {
	return decodeMappingOrList(data, json.Unmarshal, m)
}

func (m *MappingOrList) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, m.decodeFromRaw)
}

func (m *MappingOrList) decodeFromRaw(raw interface{}) error {
	m.Mapping = nil
	m.List = nil

	switch v := raw.(type) {
	case map[string]interface{}:
		values := make(map[string]*string, len(v))
		for key, value := range v {
			switch typed := value.(type) {
			case nil:
				values[key] = nil
			case string:
				copyValue := typed
				values[key] = &copyValue
			default:
				stringValue := fmt.Sprint(typed)
				values[key] = &stringValue
			}
		}
		m.Mapping = values
		return nil
	case []interface{}:
		values, err := toStringSlice(v)
		if err != nil {
			return err
		}
		m.List = values
		return nil
	default:
		return fmt.Errorf("expected mapping or string list, got %T", raw)
	}
}

func (s *StringList) UnmarshalJSON(data []byte) error {
	return decodeStringList(data, json.Unmarshal, s)
}

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, s.decodeFromRaw)
}

func (s *StringList) decodeFromRaw(raw interface{}) error {
	s.Values = nil

	switch v := raw.(type) {
	case string:
		s.Values = []string{v}
		return nil
	case []interface{}:
		values, err := toStringSlice(v)
		if err != nil {
			return err
		}
		s.Values = values
		return nil
	default:
		return fmt.Errorf("expected string or string list, got %T", raw)
	}
}

func (p *PortValue) UnmarshalJSON(data []byte) error {
	return decodePortValue(data, json.Unmarshal, p)
}

func (p *PortValue) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, p.decodeFromRaw)
}

func (p *PortValue) decodeFromRaw(raw interface{}) error {
	p.Short = ""
	p.Config = nil

	switch v := raw.(type) {
	case string:
		p.Short = v
		return nil
	case map[string]interface{}:
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var cfg ServicePortConfig
		if err := json.Unmarshal(encoded, &cfg); err != nil {
			return err
		}
		p.Config = &cfg
		return nil
	default:
		return fmt.Errorf("expected string or port mapping, got %T", raw)
	}
}

func (v *VolumeValue) UnmarshalJSON(data []byte) error {
	return decodeVolumeValue(data, json.Unmarshal, v)
}

func (v *VolumeValue) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, v.decodeFromRaw)
}

func (v *VolumeValue) decodeFromRaw(raw interface{}) error {
	v.Short = ""
	v.Config = nil

	switch typed := raw.(type) {
	case string:
		v.Short = typed
		return nil
	case map[string]interface{}:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		var cfg ServiceVolumeConfig
		if err := json.Unmarshal(encoded, &cfg); err != nil {
			return err
		}
		v.Config = &cfg
		return nil
	default:
		return fmt.Errorf("expected string or volume mapping, got %T", raw)
	}
}

func (s *ServiceNetworks) UnmarshalJSON(data []byte) error {
	return decodeServiceNetworks(data, json.Unmarshal, s)
}

func (s *ServiceNetworks) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, s.decodeFromRaw)
}

func (s *ServiceNetworks) decodeFromRaw(raw interface{}) error {
	s.Names = nil
	s.Values = nil

	switch typed := raw.(type) {
	case []interface{}:
		values, err := toStringSlice(typed)
		if err != nil {
			return err
		}
		s.Names = values
		return nil
	case map[string]interface{}:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		var values map[string]ServiceNetworkAttachment
		if err := json.Unmarshal(encoded, &values); err != nil {
			return err
		}
		s.Values = values
		return nil
	default:
		return fmt.Errorf("expected string list or network mapping, got %T", raw)
	}
}

func (d *DependsOnSpec) UnmarshalJSON(data []byte) error {
	return decodeDependsOn(data, json.Unmarshal, d)
}

func (d *DependsOnSpec) UnmarshalYAML(node *yaml.Node) error {
	return decodeYAMLNode(node, d.decodeFromRaw)
}

func (d *DependsOnSpec) decodeFromRaw(raw interface{}) error {
	d.Names = nil
	d.Values = nil

	switch typed := raw.(type) {
	case []interface{}:
		values, err := toStringSlice(typed)
		if err != nil {
			return err
		}
		d.Names = values
		return nil
	case map[string]interface{}:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		var values map[string]ServiceDependency
		if err := json.Unmarshal(encoded, &values); err != nil {
			return err
		}
		d.Values = values
		return nil
	default:
		return fmt.Errorf("expected string list or depends_on mapping, got %T", raw)
	}
}

func decodeCommandValue(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *CommandValue,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeStringList(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *StringList,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeMappingOrList(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *MappingOrList,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodePortValue(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *PortValue,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeVolumeValue(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *VolumeValue,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeServiceNetworks(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *ServiceNetworks,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeDependsOn(
	data []byte,
	unmarshal func([]byte, interface{}) error,
	target *DependsOnSpec,
) error {
	var raw interface{}
	if err := unmarshal(data, &raw); err != nil {
		return err
	}

	return target.decodeFromRaw(raw)
}

func decodeYAMLNode(node *yaml.Node, decode func(interface{}) error) error {
	var raw interface{}
	if err := node.Decode(&raw); err != nil {
		return err
	}

	return decode(raw)
}

func toStringSlice(values []interface{}) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string list item, got %T", value)
		}
		result = append(result, stringValue)
	}

	return result, nil
}

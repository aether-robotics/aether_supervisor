package types

// ServiceTarget identifies an app or a specific service within an app.
type ServiceTarget struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	Service string `json:"service,omitempty" yaml:"service,omitempty"`
}

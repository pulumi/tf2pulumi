// The config package is responsible for loading and validating the
// configuration.
package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

// NameRegexp is the regular expression that all names (modules, providers,
// resources, etc.) must follow.
var NameRegexp = regexp.MustCompile(`(?i)\A[A-Z0-9_][A-Z0-9\-\_]*\z`)

// Config is the configuration that comes from loading a collection
// of Terraform templates.
type Config struct {
	Fs afero.Fs

	// Dir is the path to the directory where this configuration was
	// loaded from. If it is blank, this configuration wasn't loaded from
	// any meaningful directory.
	Dir string

	Terraform       *Terraform
	Atlas           *AtlasConfig
	Modules         []*Module
	ProviderConfigs []*ProviderConfig
	Resources       []*Resource
	Variables       []*Variable
	Locals          []*Local
	Outputs         []*Output

	// The fields below can be filled in by loaders for validation
	// purposes.
	unknownKeys []string
}

// AtlasConfig is the configuration for building in HashiCorp's Atlas.
type AtlasConfig struct {
	Name    string
	Include []string
	Exclude []string
}

// Module is a module used within a configuration.
//
// This does not represent a module itself, this represents a module
// call-site within an existing configuration.
type Module struct {
	Name      string
	Source    string
	Version   string
	Providers map[string]string
	RawConfig *RawConfig
}

// ProviderConfig is the configuration for a resource provider.
//
// For example, Terraform needs to set the AWS access keys for the AWS
// resource provider.
type ProviderConfig struct {
	Name      string
	Alias     string
	Version   string
	RawConfig *RawConfig
}

// A resource represents a single Terraform resource in the configuration.
// A Terraform resource is something that supports some or all of the
// usual "create, read, update, delete" operations, depending on
// the given Mode.
type Resource struct {
	Mode         ResourceMode // which operations the resource supports
	Name         string
	Type         string
	RawCount     *RawConfig
	RawConfig    *RawConfig
	Provisioners []*Provisioner
	Provider     string
	DependsOn    []string
	Lifecycle    ResourceLifecycle
}

// Copy returns a copy of this Resource. Helpful for avoiding shared
// config pointers across multiple pieces of the graph that need to do
// interpolation.
func (r *Resource) Copy() *Resource {
	n := &Resource{
		Mode:         r.Mode,
		Name:         r.Name,
		Type:         r.Type,
		RawCount:     r.RawCount.Copy(),
		RawConfig:    r.RawConfig.Copy(),
		Provisioners: make([]*Provisioner, 0, len(r.Provisioners)),
		Provider:     r.Provider,
		DependsOn:    make([]string, len(r.DependsOn)),
		Lifecycle:    *r.Lifecycle.Copy(),
	}
	for _, p := range r.Provisioners {
		n.Provisioners = append(n.Provisioners, p.Copy())
	}
	copy(n.DependsOn, r.DependsOn)
	return n
}

// ResourceLifecycle is used to store the lifecycle tuning parameters
// to allow customized behavior
type ResourceLifecycle struct {
	CreateBeforeDestroy bool     `mapstructure:"create_before_destroy"`
	PreventDestroy      bool     `mapstructure:"prevent_destroy"`
	IgnoreChanges       []string `mapstructure:"ignore_changes"`
}

// Copy returns a copy of this ResourceLifecycle
func (r *ResourceLifecycle) Copy() *ResourceLifecycle {
	n := &ResourceLifecycle{
		CreateBeforeDestroy: r.CreateBeforeDestroy,
		PreventDestroy:      r.PreventDestroy,
		IgnoreChanges:       make([]string, len(r.IgnoreChanges)),
	}
	copy(n.IgnoreChanges, r.IgnoreChanges)
	return n
}

// Provisioner is a configured provisioner step on a resource.
type Provisioner struct {
	Type      string
	RawConfig *RawConfig
	ConnInfo  *RawConfig

	When      ProvisionerWhen
	OnFailure ProvisionerOnFailure
}

// Copy returns a copy of this Provisioner
func (p *Provisioner) Copy() *Provisioner {
	return &Provisioner{
		Type:      p.Type,
		RawConfig: p.RawConfig.Copy(),
		ConnInfo:  p.ConnInfo.Copy(),
		When:      p.When,
		OnFailure: p.OnFailure,
	}
}

// Variable is a module argument defined within the configuration.
type Variable struct {
	Name         string
	DeclaredType string `mapstructure:"type"`
	Default      interface{}
	Description  string
}

// Local is a local value defined within the configuration.
type Local struct {
	Name      string
	RawConfig *RawConfig
}

// Output is an output defined within the configuration. An output is
// resulting data that is highlighted by Terraform when finished. An
// output marked Sensitive will be output in a masked form following
// application, but will still be available in state.
type Output struct {
	Name        string
	DependsOn   []string
	Description string
	Sensitive   bool
	RawConfig   *RawConfig
}

// VariableType is the type of value a variable is holding, and returned
// by the Type() function on variables.
type VariableType byte

const (
	VariableTypeUnknown VariableType = iota
	VariableTypeString
	VariableTypeList
	VariableTypeMap
)

func (v VariableType) Printable() string {
	switch v {
	case VariableTypeString:
		return "string"
	case VariableTypeMap:
		return "map"
	case VariableTypeList:
		return "list"
	default:
		return "unknown"
	}
}

// ProviderConfigName returns the name of the provider configuration in
// the given mapping that maps to the proper provider configuration
// for this resource.
func ProviderConfigName(t string, pcs []*ProviderConfig) string {
	lk := ""
	for _, v := range pcs {
		k := v.Name
		if strings.HasPrefix(t, k) && len(k) > len(lk) {
			lk = k
		}
	}

	return lk
}

// A unique identifier for this module.
func (r *Module) Id() string {
	return fmt.Sprintf("%s", r.Name)
}

// Count returns the count of this resource.
func (r *Resource) Count() (int, error) {
	raw := r.RawCount.Value()
	count, ok := r.RawCount.Value().(string)
	if !ok {
		return 0, fmt.Errorf(
			"expected count to be a string or int, got %T", raw)
	}

	v, err := strconv.ParseInt(count, 0, 0)
	if err != nil {
		return 0, fmt.Errorf(
			"cannot parse %q as an integer",
			count,
		)
	}

	return int(v), nil
}

// A unique identifier for this resource.
func (r *Resource) Id() string {
	switch r.Mode {
	case ManagedResourceMode:
		return fmt.Sprintf("%s.%s", r.Type, r.Name)
	case DataResourceMode:
		return fmt.Sprintf("data.%s.%s", r.Type, r.Name)
	default:
		panic(fmt.Errorf("unknown resource mode %s", r.Mode))
	}
}

// ProviderFullName returns the full name of the provider for this resource,
// which may either be specified explicitly using the "provider" meta-argument
// or implied by the prefix on the resource type name.
func (r *Resource) ProviderFullName() string {
	return ResourceProviderFullName(r.Type, r.Provider)
}

// ResourceProviderFullName returns the full (dependable) name of the
// provider for a hypothetical resource with the given resource type and
// explicit provider string. If the explicit provider string is empty then
// the provider name is inferred from the resource type name.
func ResourceProviderFullName(resourceType, explicitProvider string) string {
	if explicitProvider != "" {
		// check for an explicit provider name, or return the original
		parts := strings.SplitAfter(explicitProvider, "provider.")
		return parts[len(parts)-1]
	}

	idx := strings.IndexRune(resourceType, '_')
	if idx == -1 {
		// If no underscores, the resource name is assumed to be
		// also the provider name, e.g. if the provider exposes
		// only a single resource of each type.
		return resourceType
	}

	return resourceType[:idx]
}

// InterpolatedVariables is a helper that returns a mapping of all the interpolated
// variables within the configuration. This is used to verify references
// are valid in the Validate step.
func (c *Config) InterpolatedVariables() map[string][]InterpolatedVariable {
	result := make(map[string][]InterpolatedVariable)
	for source, rc := range c.rawConfigs() {
		for _, v := range rc.Variables {
			result[source] = append(result[source], v)
		}
	}
	return result
}

// rawConfigs returns all of the RawConfigs that are available keyed by
// a human-friendly source.
func (c *Config) rawConfigs() map[string]*RawConfig {
	result := make(map[string]*RawConfig)
	for _, m := range c.Modules {
		source := fmt.Sprintf("module '%s'", m.Name)
		result[source] = m.RawConfig
	}

	for _, pc := range c.ProviderConfigs {
		source := fmt.Sprintf("provider config '%s'", pc.Name)
		result[source] = pc.RawConfig
	}

	for _, rc := range c.Resources {
		source := fmt.Sprintf("resource '%s'", rc.Id())
		result[source+" count"] = rc.RawCount
		result[source+" config"] = rc.RawConfig

		for i, p := range rc.Provisioners {
			subsource := fmt.Sprintf(
				"%s provisioner %s (#%d)",
				source, p.Type, i+1)
			result[subsource] = p.RawConfig
		}
	}

	for _, o := range c.Outputs {
		source := fmt.Sprintf("output '%s'", o.Name)
		result[source] = o.RawConfig
	}

	return result
}

func (c *Config) validateDependsOn(
	n string,
	v []string,
	resources map[string]*Resource,
	modules map[string]*Module) []error {
	// Verify depends on points to resources that all exist
	var errs []error
	for _, d := range v {
		// Check if we contain interpolations
		rc, err := NewRawConfig(map[string]interface{}{
			"value": d,
		})
		if err == nil && len(rc.Variables) > 0 {
			errs = append(errs, fmt.Errorf(
				"%s: depends on value cannot contain interpolations: %s",
				n, d))
			continue
		}

		// If it is a module, verify it is a module
		if strings.HasPrefix(d, "module.") {
			name := d[len("module."):]
			if _, ok := modules[name]; !ok {
				errs = append(errs, fmt.Errorf(
					"%s: resource depends on non-existent module '%s'",
					n, name))
			}

			continue
		}

		// Check resources
		if _, ok := resources[d]; !ok {
			errs = append(errs, fmt.Errorf(
				"%s: resource depends on non-existent resource '%s'",
				n, d))
		}
	}

	return errs
}

func (m *Module) mergerName() string {
	return m.Id()
}

func (m *Module) mergerMerge(other merger) merger {
	m2 := other.(*Module)

	result := *m
	result.Name = m2.Name
	result.RawConfig = result.RawConfig.merge(m2.RawConfig)

	if m2.Source != "" {
		result.Source = m2.Source
	}

	return &result
}

func (o *Output) mergerName() string {
	return o.Name
}

func (o *Output) mergerMerge(m merger) merger {
	o2 := m.(*Output)

	result := *o
	result.Name = o2.Name
	result.Description = o2.Description
	result.RawConfig = result.RawConfig.merge(o2.RawConfig)
	result.Sensitive = o2.Sensitive
	result.DependsOn = o2.DependsOn

	return &result
}

func (c *ProviderConfig) GoString() string {
	return fmt.Sprintf("*%#v", *c)
}

func (c *ProviderConfig) FullName() string {
	if c.Alias == "" {
		return c.Name
	}

	return fmt.Sprintf("%s.%s", c.Name, c.Alias)
}

func (c *ProviderConfig) mergerName() string {
	return c.Name
}

func (c *ProviderConfig) mergerMerge(m merger) merger {
	c2 := m.(*ProviderConfig)

	result := *c
	result.Name = c2.Name
	result.RawConfig = result.RawConfig.merge(c2.RawConfig)

	if c2.Alias != "" {
		result.Alias = c2.Alias
	}

	return &result
}

func (r *Resource) mergerName() string {
	return r.Id()
}

func (r *Resource) mergerMerge(m merger) merger {
	r2 := m.(*Resource)

	result := *r
	result.Mode = r2.Mode
	result.Name = r2.Name
	result.Type = r2.Type
	result.RawConfig = result.RawConfig.merge(r2.RawConfig)

	if r2.RawCount.Value() != "1" {
		result.RawCount = r2.RawCount
	}

	if len(r2.Provisioners) > 0 {
		result.Provisioners = r2.Provisioners
	}

	return &result
}

// Merge merges two variables to create a new third variable.
func (v *Variable) Merge(v2 *Variable) *Variable {
	// Shallow copy the variable
	result := *v

	// The names should be the same, but the second name always wins.
	result.Name = v2.Name

	if v2.DeclaredType != "" {
		result.DeclaredType = v2.DeclaredType
	}
	if v2.Default != nil {
		result.Default = v2.Default
	}
	if v2.Description != "" {
		result.Description = v2.Description
	}

	return &result
}

func (v *Variable) mergerName() string {
	return v.Name
}

func (v *Variable) mergerMerge(m merger) merger {
	return v.Merge(m.(*Variable))
}

// Required tests whether a variable is required or not.
func (v *Variable) Required() bool {
	return v.Default == nil
}

func (m ResourceMode) Taintable() bool {
	switch m {
	case ManagedResourceMode:
		return true
	case DataResourceMode:
		return false
	default:
		panic(fmt.Errorf("unsupported ResourceMode value %s", m))
	}
}

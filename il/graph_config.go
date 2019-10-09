package il

import "github.com/hashicorp/terraform/config"

// nodeSet is a set of Node values.
type nodeSet map[Node]struct{}

// add adds a new Node to the set.
func (s nodeSet) add(n Node) {
	s[n] = struct{}{}
}

// moduleConfig represents the configuration for a single module.
type moduleConfig interface {
	providers() []string

	bind(b *builder, name string) (*BoundMapProperty, nodeSet, error)
}

// providerConfig represents the configuration for a single provider.
type providerConfig interface {
	bind(b *builder, name string) (*BoundMapProperty, nodeSet, error)
}

// resourceConfig represents the configuration for a single resource.
type resourceConfig interface {
	providerName() string
	ignoreChanges() []string
	dependsOn() []string

	bindCount(b *builder, name string) (BoundNode, nodeSet, error)
	bindProperties(b *builder, name string, schemas Schemas, hasCount bool) (*BoundMapProperty, nodeSet, error)
}

// outputConfig represents the configuration for a single output.
type outputConfig interface {
	dependsOn() []string

	bind(b *builder, name string) (*BoundMapProperty, nodeSet, error)
}

// localConfig represents the configuration for a single local.
type localConfig interface {
	bind(b *builder, name string) (*BoundMapProperty, nodeSet, error)
}

// variableConfig represents the configuration for a single variable.
type variableConfig interface {
	bind(b *builder, name string) (BoundNode, nodeSet, error)
}

// tf11ModuleConfig is an implementation of moduleConfig that is backed by a TF11 module config.
type tf11ModuleConfig struct {
	config *config.Module
}

func (c *tf11ModuleConfig) providers() []string {
	providers := make([]string, 0, len(c.config.Providers))
	for _, p := range c.config.Providers {
		providers = append(providers, p)
	}
	return providers
}

func (c *tf11ModuleConfig) bind(b *builder, name string) (*BoundMapProperty, nodeSet, error) {
	return b.bindProperties(name, c.config.RawConfig, Schemas{}, false)
}

// tf11ProviderConfig is an implementation of providerConfig that is backed by a TF11 resource config.
type tf11ProviderConfig struct {
	config *config.ProviderConfig
}

func (c *tf11ProviderConfig) bind(b *builder, name string) (*BoundMapProperty, nodeSet, error) {
	return b.bindProperties(name, c.config.RawConfig, Schemas{}, false)
}

// tf11ResourceConfig is an implementation of resourceConfig that is backed by a TF11 resource config.
type tf11ResourceConfig struct {
	config *config.Resource
}

func (c *tf11ResourceConfig) providerName() string {
	return c.config.ProviderFullName()
}

func (c *tf11ResourceConfig) ignoreChanges() []string {
	return c.config.Lifecycle.IgnoreChanges
}

func (c *tf11ResourceConfig) dependsOn() []string {
	return c.config.DependsOn
}

func (c *tf11ResourceConfig) bindCount(b *builder, name string) (BoundNode, nodeSet, error) {
	return b.bindProperty(name, c.config.RawCount.Value(), Schemas{}, false)
}

func (c *tf11ResourceConfig) bindProperties(
	b *builder, name string, schemas Schemas, hasCount bool) (*BoundMapProperty, nodeSet, error) {

	return b.bindProperties(name, c.config.RawConfig, schemas, hasCount)
}

// tf11OutputConfig is an implementation of outputConfig that is backed by a TF11 output config.
type tf11OutputConfig struct {
	config *config.Output
}

func (c *tf11OutputConfig) dependsOn() []string {
	return c.config.DependsOn
}

func (c *tf11OutputConfig) bind(b *builder, name string) (*BoundMapProperty, nodeSet, error) {
	return b.bindProperties(name, c.config.RawConfig, Schemas{}, false)
}

// tf11LocalConfig is an implementation of localConfig that is backed by a TF11 local config.
type tf11LocalConfig struct {
	config *config.Local
}

func (c *tf11LocalConfig) bind(b *builder, name string) (*BoundMapProperty, nodeSet, error) {
	return b.bindProperties(name, c.config.RawConfig, Schemas{}, false)
}

// tf11VariableConfig is an implementation of variableConfig that is backed by a TF11 variable config.
type tf11VariableConfig struct {
	config *config.Variable
}

func (c *tf11VariableConfig) bind(b *builder, name string) (BoundNode, nodeSet, error) {
	return b.bindProperty(name, c.config.Default, Schemas{}, false)
}

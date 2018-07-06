package il

import (
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/workspace"
	"github.com/ugorji/go/codec"
)

// TODO
// - provisioners

// A Graph is the analyzed form of the configuration for a single Terraform module.
type Graph struct {
	// Tree is the module's entry in the module tree. The tree is used e.g. to determine the module's name.
	Tree *module.Tree
	// Modules maps from module name to module node for this module's module instantiations. This map is used to
	// bind a module variable access in an interpolation to the corresponding module node.
	Modules map[string]*ModuleNode
	// Providers maps from provider name to provider node for this module's provider instantiations. This map is
	// used to bind a provider reference to the corresponding provider node.
	Providers map[string]*ProviderNode
	// Resources maps from resource name to module node for this module's module instantiations. This map is used
	// to bind a resource variable access in an interpolation to the corresponding resource node.
	Resources map[string]*ResourceNode
	// Outputs maps from output name to output node for this module's outputs.
	Outputs map[string]*OutputNode
	// Locals maps from local value name to local value node for this module's local values. This map is used to bind a
	// local variable access in an interpolation to the corresponding local value node.
	Locals map[string]*LocalNode
	// Variables maps from variable name to variable node for this module's variables. This map is used to bind a
	// variable access in an interpolation to the corresponding variable node.
	Variables map[string]*VariableNode
}

// A Node represents a single node in a dependency graph. A node is connected to other nodes by dependency edges.
// The set of nodes and edges forms a DAG. Each concrete node type corresponds to a particular Terraform concept;
// ResourceNode, for example, represents a resource in a Terraform configuration.
//
// In general, a node's dependencies are the union from its implicit dependencies (i.e. the nodes referenced by the
// interpolations in its properties, if any) and its explicit dependencies.
type Node interface {
	// Dependencies returns the list of nodes the node depends on.
	Dependencies() []Node
	// sortKey returns the key that should be used when sorting this node (e.g. to ensure a stable order for code
	// generation).
	sortKey() string
}

// A ModuleNode is the analyzed form of a module instantiation in a Terraform configuration.
type ModuleNode struct {
	// Config is the module's raw Terraform configuration.
	Config *config.Module
	// Deps is the list of the module's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Properties is the bound form of the module's configuration properties.
	Properties *BoundMapProperty
}

// A ProviderNode is the analyzed form of a provider instantiation in a Terraform configuration.
type ProviderNode struct {
	// Config is the provider's raw Terraform configuration.
	Config *config.ProviderConfig
	// Deps is the list of the provider's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Properties is the bound form of the provider's configuration properties.
	Properties *BoundMapProperty
	// Info is the set of Pulumi-specific information about this particular resource provider. Of particular interest
	// is per-{resource,data source} schema information, which is used to calculate names and types for resources and
	// their properties.
	Info *tfbridge.ProviderInfo
}

// A ResourceNode is the analyzed form of a resource or data source instatiation in a Terraform configuration. In
// keeping with Terraform's internal terminology, these concepts will be collectively referred to as resources: when it
// is necessary to differentiate between the two, the former are referred to as "managed resources" and the latter as
// "data resources".
type ResourceNode struct {
	// Config is the resource's raw Terraform configuration.
	Config *config.Resource
	// Provider is a reference to the resource's provider. Consumers of this package will never observe a nil value in
	// this field.
	Provider *ProviderNode
	// Deps is the list of the resource's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// ExplicitDeps is the list of the resource's explicit dependencies. This is a subset of Deps.
	ExplicitDeps []Node
	// Count is the bound form of the resource's count property.
	Count BoundNode
	// Properties is the bound form of the resource's configuration properties.
	Properties *BoundMapProperty
}

// An OutputNode is the analyzed form of an output in a Terraform configuration. An OutputNode may never be referenced
// by another node, as its value is not nameable in a Terraform configuration.
type OutputNode struct {
	// Config is the output's raw Terraform configuration.
	Config *config.Output
	// Deps is the list of the output's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// ExplicitDeps is the list of the output's explicit dependencies. This is a subset of Deps.
	ExplicitDeps []Node
	// Value is the bound from of the output's value.
	Value BoundNode
}

// A LocalNode is the analyzed form of a local value in a Terraform configuration.
type LocalNode struct {
	// Config is the local value's raw Terraform configuration.
	Config *config.Local
	// Deps is the list of the local value's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Value is the bound form of the local value's value.
	Value BoundNode
}

// A VariableNode is the analyzed form of a Terraform variable. A VariableNode's list of dependencies is always empty.
type VariableNode struct {
	// Config is the variable's raw Terraform configuration.
	Config *config.Variable
	// DefaultValue is the bound form of the variable's default value (if any).
	DefaultValue BoundNode
}

// Depdendencies returns the list of nodes the module depends on.
func (m *ModuleNode) Dependencies() []Node {
	return m.Deps
}

func (m *ModuleNode) sortKey() string {
	return "m" + m.Config.Name
}

// Depdendencies returns the list of nodes the provider depends on.
func (p *ProviderNode) Dependencies() []Node {
	return p.Deps
}

func (p *ProviderNode) sortKey() string {
	return "p" + p.Config.Name
}

// Depdendencies returns the list of nodes the resource depends on.
func (r *ResourceNode) Dependencies() []Node {
	return r.Deps
}

// Schemas returns the Terraform and Pulumi schemas for this resource. These schemas can are principally used to
// calculate the types and names of a resource's properties during binding and code generation.
func (r *ResourceNode) Schemas() Schemas {
	switch {
	case r.Provider == nil || r.Provider.Info == nil:
		return Schemas{}
	case r.Config.Mode == config.ManagedResourceMode:
		resInfo := r.Provider.Info.Resources[r.Config.Type]
		return Schemas{
			TFRes:  r.Provider.Info.P.ResourcesMap[r.Config.Type],
			Pulumi: &tfbridge.SchemaInfo{Fields: resInfo.Fields},
		}
	default:
		dsInfo := r.Provider.Info.DataSources[r.Config.Type]
		return Schemas{
			TFRes:  r.Provider.Info.P.DataSourcesMap[r.Config.Type],
			Pulumi: &tfbridge.SchemaInfo{Fields: dsInfo.Fields},
		}
	}
}

// Tok returns the Pulumi token for this resource. These tokens are of the form "provider:module/func:member".
func (r *ResourceNode) Tok() (string, bool) {
	switch {
	case r.Provider == nil || r.Provider.Info == nil:
		return "", false
	case r.Config.Mode == config.ManagedResourceMode:
		return string(r.Provider.Info.Resources[r.Config.Type].Tok), true
	default:
		return string(r.Provider.Info.DataSources[r.Config.Type].Tok), true
	}
}

func (r *ResourceNode) sortKey() string {
	return "r" + r.Config.Id()
}

// Depdendencies returns the list of nodes the output depends on.
func (o *OutputNode) Dependencies() []Node {
	return o.Deps
}

func (o *OutputNode) sortKey() string {
	return "o" + o.Config.Name
}

// Depdendencies returns the list of nodes the local value depends on.
func (l *LocalNode) Dependencies() []Node {
	return l.Deps
}

func (l *LocalNode) sortKey() string {
	return "l" + l.Config.Name
}

// Depdendencies returns the list of nodes the variable depends on. This list is always emtpy.
func (v *VariableNode) Dependencies() []Node {
	return nil
}

func (v *VariableNode) sortKey() string {
	return "v" + v.Config.Name
}

// A builder is a temporary structure used to hold the contents of a graph that while it is under construction. The
// various fields are aligned with their similarly-named peers in Graph.
type builder struct {
	modules   map[string]*ModuleNode
	providers map[string]*ProviderNode
	resources map[string]*ResourceNode
	outputs   map[string]*OutputNode
	locals    map[string]*LocalNode
	variables map[string]*VariableNode
}

func newBuilder() *builder {
	return &builder{
		modules:   make(map[string]*ModuleNode),
		providers: make(map[string]*ProviderNode),
		resources: make(map[string]*ResourceNode),
		outputs:   make(map[string]*OutputNode),
		locals:    make(map[string]*LocalNode),
		variables: make(map[string]*VariableNode),
	}
}

// bindProperty binds a paroperty value with the given schemas. If hasCountIndex is true, this property's
// interpolations may legally contain references to their container's count variable (i.e. `count,index`).
//
// In addition to the bound property, this function returns the set of nodes referenced by the property's
// interpolations. If v is nil, the returned BoundNode will also be nil.
func (b *builder) bindProperty(v interface{}, sch Schemas, hasCountIndex bool) (BoundNode, map[Node]struct{}, error) {
	if v == nil {
		return nil, nil, nil
	}

	// Bind the value.
	binder := &propertyBinder{
		builder:       b,
		hasCountIndex: hasCountIndex,
	}
	prop, err := binder.bindProperty(reflect.ValueOf(v), sch)
	if err != nil {
		return nil, nil, err
	}

	// Walk the bound value and collect its dependencies.
	deps := make(map[Node]struct{})
	VisitBoundNode(prop, IdentityVisitor, func(n BoundNode) (BoundNode, error) {
		if v, ok := n.(*BoundVariableAccess); ok {
			if v.ILNode != nil {
				deps[v.ILNode] = struct{}{}
			}
		}
		return n, nil
	})

	return prop, deps, nil
}

// bindProperties binds the set of properties represented by the given Terraform config with using the given schema. If
// hasCountIndex is true, this property's interpolations may legally contain references to their container's count
// variable (i.e. `count,index`).
//
// In addition to the bound property, this function returns the set of nodes referenced by the property's
// interpolations.
func (b *builder) bindProperties(raw *config.RawConfig, sch Schemas, hasCountIndex bool) (*BoundMapProperty, map[Node]struct{}, error) {
	v, deps, err := b.bindProperty(raw.Raw, sch, hasCountIndex)
	if err != nil {
		return nil, nil, err
	}
	return v.(*BoundMapProperty), deps, nil
}

// sortableNodes is a helper type that allows a slice of nodes to be passed to sort.Sort. This is used e.g. to ensure a
// consistent order for a node's dependency list.
type sortableNodes []Node

func (sn sortableNodes) Len() int {
	return len(sn)
}

func (sn sortableNodes) Less(i, j int) bool {
	return sn[i].sortKey() < sn[j].sortKey()
}

func (sn sortableNodes) Swap(i, j int) {
	sn[i], sn[j] = sn[j], sn[i]
}

// buildDeps calculates the union of a node's implicit and explicit dependencies. It returns this union as a list of
// Nodes as well as the list of the node's explicit dependencies. This function will fail if a node referenced in the
// list of explicit dependencies is not present in the graph.
func (b *builder) buildDeps(deps map[Node]struct{}, dependsOn []string) ([]Node, []Node, error) {
	sort.Strings(dependsOn)

	explicitDeps := make([]Node, len(dependsOn))
	for i, name := range dependsOn {
		if strings.HasPrefix(name, "module.") {
			return nil, nil, errors.Errorf("module references are not yet supported (%v)", name)
		}
		r, ok := b.resources[name]
		if !ok {
			return nil, nil, errors.Errorf("unknown resource %v", name)
		}
		deps[r], explicitDeps[i] = struct{}{}, r
	}

	allDeps := make([]Node, 0, len(deps))
	for n := range deps {
		allDeps = append(allDeps, n)
	}

	sort.Sort(sortableNodes(allDeps))

	return allDeps, explicitDeps, nil
}

// fixupTFResource recursively fixes up a resource schema's Elem values.
func fixupTFResource(r *schema.Resource) error {
	for _, s := range r.Schema {
		if err := fixupTFSchema(s); err != nil {
			return err
		}
	}
	return nil
}

// fixupTFSchema turns a schema's Elem value from a raw map produced by codec into a proper schema.Schema or
// schema.Resource value using mapstructure.
func fixupTFSchema(s *schema.Schema) error {
	rawElem, ok := s.Elem.(map[interface{}]interface{})
	if !ok {
		return nil
	}

	if _, hasType := rawElem["type"]; hasType {
		var elemSch schema.Schema
		if err := mapstructure.Decode(rawElem, &elemSch); err != nil {
			return err
		}
		fixupTFSchema(&elemSch)
		s.Elem = &elemSch
	} else {
		var elemRes schema.Resource
		if err := mapstructure.Decode(rawElem, &elemRes); err != nil {
			return err
		}
		fixupTFResource(&elemRes)
		s.Elem = &elemRes
	}

	return nil
}

// getProviderInfo fetches the tfbridge information for a particular provider. It does so by launching the provider
// plugin with the "-get-provider-info" flag and deserializing the JSON representation dumped to stdout.
func getProviderInfo(p *ProviderNode) (*tfbridge.ProviderInfo, error) {
	if info, ok := builtinProviderInfo[p.Config.Name]; ok {
		return info, nil
	}

	_, path, err := workspace.GetPluginPath(workspace.ResourcePlugin, p.Config.Name, nil)
	if err != nil {
		return nil, err
	} else if path == "" {
		return nil, errors.Errorf("could not find plugin for provider %s", p.Config.Name)
	}

	// Run the plugin and decode its provider config.
	cmd := exec.Command(path, "-get-provider-info")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var info tfbridge.ProviderInfo
	json := &codec.JsonHandle{}
	err = codec.NewDecoder(out, json).Decode(&info)

	if cErr := cmd.Wait(); cErr != nil {
		return nil, cErr
	}
	if err != nil {
		return nil, err
	}

	// Fix up schema elems.
	for _, r := range info.P.ResourcesMap {
		fixupTFResource(r)
	}

	return &info, nil
}

// buildModule binds the given module node's properties and computes its dependency edges.
func (b *builder) buildModule(m *ModuleNode) error {
	props, deps, err := b.bindProperties(m.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil)
	contract.Assert(err == nil)

	m.Properties, m.Deps = props, allDeps
	return nil
}

// buildProvider fetches the given provider's tfbridge data, binds its properties, and computes its dependency edges.
func (b *builder) buildProvider(p *ProviderNode) error {
	info, err := getProviderInfo(p)
	if err != nil {
		return err
	}
	p.Info = info

	props, deps, err := b.bindProperties(p.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil)
	contract.Assert(err == nil)

	p.Properties, p.Deps = props, allDeps
	return nil
}

// ensureProvider ensures that the given resource node's provider field is non-nil, This function should be called
// before accessing a ResourceNode's Provider field until all resource nodes have been built.
func (b *builder) ensureProvider(r *ResourceNode) error {
	if r.Provider != nil {
		return nil
	}

	providerName := r.Config.ProviderFullName()
	p, ok := b.providers[providerName]
	if !ok {
		// It is possible to reference a provider that is not present in the Terraform configuration. In this case,
		// we create a new provider node with an empty configuration and insert it into the graph.
		rawConfig, err := config.NewRawConfig(map[string]interface{}{})
		if err != nil {
			return err
		}

		p = &ProviderNode{
			Config: &config.ProviderConfig{
				Name:      providerName,
				RawConfig: rawConfig,
			},
		}
		b.providers[providerName] = p
		if err = b.buildProvider(p); err != nil {
			return err
		}
	}
	r.Provider = p
	return nil
}

// buildResource binds a resource's properties (including its count property) and computes its dependency edges.
func (b *builder) buildResource(r *ResourceNode) error {
	b.ensureProvider(r)

	count, countDeps, err := b.bindProperty(r.Config.RawCount.Value(), Schemas{}, false)
	if err != nil {
		return err
	}
	// If the count is a string that can be parsed as an integer, use the result of the parse as the count. If the
	// count is exactly one, set the count to nil.
	if countLit, ok := count.(*BoundLiteral); ok && countLit.ExprType == TypeString {
		countInt, err := strconv.ParseInt(countLit.Value.(string), 0, 0)
		if err == nil {
			if countInt == 1 {
				count = nil
			} else {
				count = &BoundLiteral{ExprType: TypeNumber, Value: float64(countInt)}
			}
		}
	}

	props, deps, err := b.bindProperties(r.Config.RawConfig, r.Schemas(), count != nil)
	if err != nil {
		return err
	}

	// Merge the count dependencies into the overall dependency set and compute the final dependency lists.
	for k := range countDeps {
		deps[k] = struct{}{}
	}
	allDeps, explicitDeps, err := b.buildDeps(deps, r.Config.DependsOn)
	if err != nil {
		return err
	}
	r.Count, r.Properties, r.Deps, r.ExplicitDeps = count, props, allDeps, explicitDeps
	return nil
}

// buildOutput binds an output's value and computes its dependency edges.
func (b *builder) buildOutput(o *OutputNode) error {
	props, deps, err := b.bindProperties(o.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, explicitDeps, err := b.buildDeps(deps, o.Config.DependsOn)
	if err != nil {
		return err
	}

	// In general, an output should have a single property named "value". If this is the case, promote it to the
	// output's value.
	value := BoundNode(props)
	if len(props.Elements) == 1 {
		if v, ok := props.Elements["value"]; ok {
			value = v
		}
	}

	o.Value, o.Deps, o.ExplicitDeps = value, allDeps, explicitDeps
	return nil
}

// buildLocal binds a local value's value and computes its dependency edges.
func (b *builder) buildLocal(l *LocalNode) error {
	props, deps, err := b.bindProperties(l.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil)
	contract.Assert(err == nil)

	// In general, a local should have a single property named "value". If this is the case, promote it to the
	// local's value.
	value := BoundNode(props)
	if len(props.Elements) == 1 {
		if v, ok := props.Elements["value"]; ok {
			value = v
		}
	}

	l.Value, l.Deps = value, allDeps
	return nil
}

// buildVariable builds a variable's default value (if any). This value must not depend on any other nodes.
func (b *builder) buildVariable(v *VariableNode) error {
	defaultValue, deps, err := b.bindProperty(v.Config.Default, Schemas{}, false)
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return errors.Errorf("variables may not depend on other nodes (%v)", v.Config.Name)
	}
	v.DefaultValue = defaultValue
	return nil
}

// BuildGraph analyzes the various entities present in the given module's configuration and constructs the
// corresponding dependency graph. Building the graph involves binding each entity's properties (if any) and
// computing its list of dependency edges.
func BuildGraph(tree *module.Tree) (*Graph, error) {
	b := newBuilder()

	conf := tree.Config()

	// Next create our nodes.
	for _, m := range conf.Modules {
		b.modules[m.Name] = &ModuleNode{Config: m}
	}
	for _, p := range conf.ProviderConfigs {
		b.providers[p.Name] = &ProviderNode{Config: p}
	}
	for _, r := range conf.Resources {
		b.resources[r.Id()] = &ResourceNode{Config: r}
	}
	for _, o := range conf.Outputs {
		b.outputs[o.Name] = &OutputNode{Config: o}
	}
	for _, l := range conf.Locals {
		b.locals[l.Name] = &LocalNode{Config: l}
	}
	for _, v := range conf.Variables {
		b.variables[v.Name] = &VariableNode{Config: v}
	}

	// Now bind each node's properties and compute any dependency edges.
	for _, m := range b.modules {
		if err := b.buildModule(m); err != nil {
			return nil, err
		}
	}
	for _, p := range b.providers {
		if err := b.buildProvider(p); err != nil {
			return nil, err
		}
	}
	for _, r := range b.resources {
		if err := b.buildResource(r); err != nil {
			return nil, err
		}
	}
	for _, o := range b.outputs {
		if err := b.buildOutput(o); err != nil {
			return nil, err
		}
	}
	for _, l := range b.locals {
		if err := b.buildLocal(l); err != nil {
			return nil, err
		}
	}
	for _, v := range b.variables {
		if err := b.buildVariable(v); err != nil {
			return nil, err
		}
	}

	// Put the graph together
	return &Graph{
		Tree:      tree,
		Modules:   b.modules,
		Providers: b.providers,
		Resources: b.resources,
		Outputs:   b.outputs,
		Locals:    b.locals,
		Variables: b.variables,
	}, nil
}

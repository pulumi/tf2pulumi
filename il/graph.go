// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package il

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/hcl/token"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/internal/config"
	"github.com/pulumi/tf2pulumi/internal/config/module"
)

// TODO
// - provisioners

// A Graph is the analyzed form of the configuration for a single Terraform module.
type Graph struct {
	// Tree is the module's entry in the module tree. The tree is used e.g. to determine the module's name.
	Tree *module.Tree
	// Name is the name of the module. May be the empty string when IsRoot is true.
	Name string
	// IsRoot is true if this is the root module.
	IsRoot bool
	// Path is the path to this module's directory.
	Path string
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
	locatable
	commentable

	// Dependencies returns the list of nodes the node depends on.
	Dependencies() []Node
	// ID returns the unique ID for this node.
	ID() string
	// displayName returns the display name of this node
	displayName() string
}

// A ModuleNode is the analyzed form of a module instantiation in a Terraform configuration.
type ModuleNode struct {
	// Config is the module's raw Terraform configuration.
	Config *config.Module
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Deps is the list of the module's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Name is the name of the module.
	Name string
	// Properties is the bound form of the module's configuration properties.
	Properties *BoundMapProperty
}

// A ProviderNode is the analyzed form of a provider instantiation in a Terraform configuration.
type ProviderNode struct {
	// Config is the provider's raw Terraform configuration.
	Config *config.ProviderConfig
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Deps is the list of the provider's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Name is the name of the provider.
	Name string
	// Alias is the provider's alias, if any.
	Alias string
	// Properties is the bound form of the provider's configuration properties.
	Properties *BoundMapProperty
	// Info is the set of Pulumi-specific information about this particular resource provider. Of particular interest
	// is per-{resource,data source} schema information, which is used to calculate names and types for resources and
	// their properties.
	Info *tfbridge.ProviderInfo
	// PluginName is the name of the Pulumi plugin associated with this provider.
	PluginName string
	// Implicit is true if this provider node was generated by an implicit provider block.
	Implicit bool
}

// A ResourceNode is the analyzed form of a resource or data source instatiation in a Terraform configuration. In
// keeping with Terraform's internal terminology, these concepts will be collectively referred to as resources: when it
// is necessary to differentiate between the two, the former are referred to as "managed resources" and the latter as
// "data resources".
type ResourceNode struct {
	// Config is the resource's raw Terraform configuration.
	Config *config.Resource
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Deps is the list of the resource's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// ExplicitDeps is the list of the resource's explicit dependencies. This is a subset of Deps.
	ExplicitDeps []Node
	// Type is the type of the resource.
	Type string
	// Name is the name of the resource.
	Name string
	// IsDataSource is true if this resource represents a data source invocation.
	IsDataSource bool
	// Provider is a reference to the resource's provider. Consumers of this package will never observe a nil value in
	// this field.
	Provider *ProviderNode
	// Count is the bound form of the resource's count property.
	Count BoundNode
	// Properties is the bound form of the resource's configuration properties.
	Properties *BoundMapProperty
	// Timeouts is the bound set of timeout data, if any.
	Timeouts *BoundMapProperty
	// IgnoreChanges is the bound list of properties with ignored changes, if any.
	IgnoreChanges []string
}

// An OutputNode is the analyzed form of an output in a Terraform configuration. An OutputNode may never be referenced
// by another node, as its value is not nameable in a Terraform configuration.
type OutputNode struct {
	// Config is the output's raw Terraform configuration.
	Config *config.Output
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Deps is the list of the output's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// ExplicitDeps is the list of the output's explicit dependencies. This is a subset of Deps.
	ExplicitDeps []Node
	// Name is the name of this output.
	Name string
	// Value is the bound from of the output's value.
	Value BoundNode
}

// A LocalNode is the analyzed form of a local value in a Terraform configuration.
type LocalNode struct {
	// Config is the local value's raw Terraform configuration.
	Config *config.Local
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Deps is the list of the local value's dependencies as implied by the nodes referenced by its configuration.
	Deps []Node
	// Name is the name of this local.
	Name string
	// Value is the bound form of the local value's value.
	Value BoundNode
}

// A VariableNode is the analyzed form of a Terraform variable. A VariableNode's list of dependencies is always empty.
type VariableNode struct {
	// Config is the variable's raw Terraform configuration.
	Config *config.Variable
	// Location is the location of this node's definition in the original Terraform configuration.
	Location token.Pos
	// Comments is the set of comments associated with this node, if any.
	Comments *Comments
	// Name is the name of this variable.
	Name string
	// DefaultValue is the bound form of the variable's default value (if any).
	DefaultValue BoundNode
}

// nodeSet is a set of Node values.
type nodeSet map[Node]struct{}

// add adds a new Node to the set.
func (s nodeSet) add(n Node) {
	s[n] = struct{}{}
}

// Depdendencies returns the list of nodes the module depends on.
func (m *ModuleNode) Dependencies() []Node {
	return m.Deps
}

func (m *ModuleNode) ID() string {
	return "m" + m.Name
}

func (m *ModuleNode) displayName() string {
	return "module " + m.Name
}

func (m *ModuleNode) GetLocation() token.Pos {
	return m.Location
}

func (m *ModuleNode) setLocation(l token.Pos) {
	m.Location = l
}

func (m *ModuleNode) setComments(c *Comments) {
	m.Comments = c
}

// fullName returns the full name (name + alias) of this provider.
func (p *ProviderNode) fullName() string {
	if p.Alias == "" {
		return p.Name
	}
	return fmt.Sprintf("%s.%s", p.Name, p.Alias)
}

// Depdendencies returns the list of nodes the provider depends on.
func (p *ProviderNode) Dependencies() []Node {
	return p.Deps
}

func (p *ProviderNode) ID() string {
	return "p" + p.fullName()
}

func (p *ProviderNode) displayName() string {
	return "provider " + p.fullName()
}

func (p *ProviderNode) GetLocation() token.Pos {
	return p.Location
}

func (p *ProviderNode) setLocation(l token.Pos) {
	p.Location = l
}

func (p *ProviderNode) setComments(c *Comments) {
	p.Comments = c
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
	case !r.IsDataSource:
		schemaInfo := &tfbridge.SchemaInfo{}
		if resInfo, ok := r.Provider.Info.Resources[r.Type]; ok {
			schemaInfo.Fields = resInfo.Fields
		}
		return Schemas{
			TFRes:  r.Provider.Info.P.ResourcesMap[r.Type],
			Pulumi: schemaInfo,
		}
	default:
		schemaInfo := &tfbridge.SchemaInfo{}
		if dsInfo, ok := r.Provider.Info.DataSources[r.Type]; ok {
			schemaInfo.Fields = dsInfo.Fields
		}
		return Schemas{
			TFRes:  r.Provider.Info.P.DataSourcesMap[r.Type],
			Pulumi: schemaInfo,
		}
	}
}

// Tok returns the Pulumi token for this resource. These tokens are of the form "provider:module/func:member".
func (r *ResourceNode) Tok() (string, bool) {
	switch {
	case r.Provider == nil || r.Provider.Info == nil:
		return "", false
	case !r.IsDataSource:
		if resInfo, ok := r.Provider.Info.Resources[r.Type]; ok {
			return string(resInfo.Tok), true
		}
		return "", false
	default:
		if dsInfo, ok := r.Provider.Info.DataSources[r.Type]; ok {
			return string(dsInfo.Tok), true
		}
		return "", false
	}
}

func (r *ResourceNode) resourceID() string {
	if r.IsDataSource {
		return fmt.Sprintf("data.%s.%s", r.Type, r.Name)
	}
	return fmt.Sprintf("%s.%s", r.Type, r.Name)
}

func (r *ResourceNode) ID() string {
	return "r" + r.resourceID()
}

func (r *ResourceNode) displayName() string {
	return "resource " + r.resourceID()
}

func (r *ResourceNode) GetLocation() token.Pos {
	return r.Location
}

func (r *ResourceNode) setLocation(l token.Pos) {
	r.Location = l
}

func (r *ResourceNode) setComments(c *Comments) {
	r.Comments = c
}

// Depdendencies returns the list of nodes the output depends on.
func (o *OutputNode) Dependencies() []Node {
	return o.Deps
}

func (o *OutputNode) ID() string {
	return "o" + o.Name
}

func (o *OutputNode) displayName() string {
	return "output " + o.Name
}

func (o *OutputNode) GetLocation() token.Pos {
	return o.Location
}

func (o *OutputNode) setLocation(l token.Pos) {
	o.Location = l
}

func (o *OutputNode) setComments(c *Comments) {
	o.Comments = c
}

// Depdendencies returns the list of nodes the local value depends on.
func (l *LocalNode) Dependencies() []Node {
	return l.Deps
}

func (l *LocalNode) ID() string {
	return "l" + l.Name
}

func (l *LocalNode) displayName() string {
	return "local " + l.Name
}

func (l *LocalNode) GetLocation() token.Pos {
	return l.Location
}

func (l *LocalNode) setLocation(loc token.Pos) {
	l.Location = loc
}

func (l *LocalNode) setComments(c *Comments) {
	l.Comments = c
}

// Depdendencies returns the list of nodes the variable depends on. This list is always empty.
func (v *VariableNode) Dependencies() []Node {
	return nil
}

func (v *VariableNode) ID() string {
	return "v" + v.Name
}

func (v *VariableNode) displayName() string {
	return "variable " + v.Name
}

func (v *VariableNode) GetLocation() token.Pos {
	return v.Location
}

func (v *VariableNode) setLocation(l token.Pos) {
	v.Location = l
}

func (v *VariableNode) setComments(c *Comments) {
	v.Comments = c
}

// A builder is a temporary structure used to hold the contents of a graph that while it is under construction. The
// various fields are aligned with their similarly-named peers in Graph.
type builder struct {
	logger                *log.Logger
	allowMissingProviders bool
	allowMissingVariables bool

	providerInfo ProviderInfoSource
	modules      map[string]*ModuleNode
	providers    map[string]*ProviderNode
	resources    map[string]*ResourceNode
	outputs      map[string]*OutputNode
	locals       map[string]*LocalNode
	variables    map[string]*VariableNode

	binding map[Node]bool
	bound   map[Node]bool
}

func newBuilder(opts *BuildOptions) *builder {
	allowMissingProviders, allowMissingVariables := false, false
	if opts != nil {
		allowMissingProviders, allowMissingVariables = opts.AllowMissingProviders, opts.AllowMissingVariables
	}

	providerInfo := PluginProviderInfoSource
	if opts != nil && opts.ProviderInfoSource != nil {
		providerInfo = opts.ProviderInfoSource
	}

	var logger *log.Logger
	if opts != nil {
		logger = opts.Logger
	}

	return &builder{
		logger:                logger,
		allowMissingProviders: allowMissingProviders,
		allowMissingVariables: allowMissingVariables,

		providerInfo: providerInfo,
		modules:      make(map[string]*ModuleNode),
		providers:    make(map[string]*ProviderNode),
		resources:    make(map[string]*ResourceNode),
		outputs:      make(map[string]*OutputNode),
		locals:       make(map[string]*LocalNode),
		variables:    make(map[string]*VariableNode),

		binding: make(map[Node]bool),
		bound:   make(map[Node]bool),
	}
}

// logf writes a formatted message to the configured logger.
func (b *builder) logf(format string, arguments ...interface{}) {
	if b.logger != nil {
		b.logger.Printf(format, arguments...)
		return
	}

	log.Printf(format, arguments...)
}

// bindProperty binds a paroperty value with the given schemas. If hasCountIndex is true, this property's
// interpolations may legally contain references to their container's count variable (i.e. `count,index`).
//
// In addition to the bound property, this function returns the set of nodes referenced by the property's
// interpolations. If v is nil, the returned BoundNode will also be nil.
func (b *builder) bindProperty(
	path string, v interface{}, sch Schemas, hasCountIndex bool) (BoundNode, nodeSet, error) {

	if v == nil {
		return nil, nil, nil
	}

	// Bind the value.
	binder := &propertyBinder{
		builder:       b,
		hasCountIndex: hasCountIndex,
	}
	prop, err := binder.bindProperty(path, reflect.ValueOf(v), sch)
	if err != nil {
		return nil, nil, err
	}

	// Walk the bound value and collect its dependencies.
	deps := make(nodeSet)
	_, err = VisitBoundNode(prop, IdentityVisitor, func(n BoundNode) (BoundNode, error) {
		if v, ok := n.(*BoundVariableAccess); ok {
			if v.ILNode != nil {
				deps.add(v.ILNode)
			}
		}
		return n, nil
	})
	contract.Assert(err == nil)

	return prop, deps, nil
}

// bindProperties binds the set of properties represented by the given Terraform config with using the given schema. If
// hasCountIndex is true, this property's interpolations may legally contain references to their container's count
// variable (i.e. `count,index`).
//
// In addition to the bound property, this function returns the set of nodes referenced by the property's
// interpolations.
func (b *builder) bindProperties(name string, raw *config.RawConfig, sch Schemas,
	hasCountIndex bool) (*BoundMapProperty, nodeSet, error) {

	v, deps, err := b.bindProperty(name, raw.Raw, sch, hasCountIndex)
	if err != nil {
		return nil, nil, err
	}
	return v.(*BoundMapProperty), deps, nil
}

// buildDeps calculates the union of a node's implicit and explicit dependencies. It returns this union as a list of
// Nodes as well as the list of the node's explicit dependencies. This function will fail if a node referenced in the
// list of explicit dependencies is not present in the graph.
func (b *builder) buildDeps(deps nodeSet, dependsOn []string, providers []string) ([]Node, []Node, error) {
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
		deps.add(r)
		explicitDeps[i] = r
	}

	// Explicitly add the provider as a dependency.
	for _, providerName := range providers {
		if p, ok := b.providers[providerName]; ok {
			deps.add(p)
		}
	}

	allDeps := make([]Node, 0, len(deps))
	for n := range deps {
		allDeps = append(allDeps, n)
	}

	return allDeps, explicitDeps, nil
}

// getProviderInfo fetches the tfbridge information for a particular provider. It does so by launching the provider
// plugin with the "-get-provider-info" flag and deserializing the JSON representation dumped to stdout.
func (b *builder) getProviderInfo(p *ProviderNode) (*tfbridge.ProviderInfo, string, error) {
	if info, ok := builtinProviderInfo[p.Name]; ok {
		return info, p.Name, nil
	}

	info, err := b.providerInfo.GetProviderInfo(p.Name)
	if err != nil {
		return nil, "", err
	}
	packageName, ok := pluginNames[p.Name]
	if !ok {
		packageName = p.Name
	}
	return info, packageName, nil
}

// buildModule binds the given module node's properties and computes its dependency edges.
func (b *builder) buildModule(m *ModuleNode) error {
	props, deps, err := b.bindProperties(m.Name, m.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}

	providers := make([]string, 0, len(m.Config.Providers))
	for _, p := range m.Config.Providers {
		providers = append(providers, p)
	}
	allDeps, _, err := b.buildDeps(deps, nil, providers)
	contract.Assert(err == nil)

	m.Properties, m.Deps = props, allDeps
	return nil
}

// buildProvider fetches the given provider's tfbridge data, binds its properties, and computes its dependency edges.
func (b *builder) buildProvider(p *ProviderNode) error {
	info, pluginName, err := b.getProviderInfo(p)
	if err != nil {
		if !b.allowMissingProviders {
			return err
		}

		b.logf("warning: %v\ngenerated code for resources using this provider may be incorrect", err)
		pluginName = p.Name
	}
	p.Info, p.PluginName = info, pluginName

	props, deps, err := b.bindProperties(p.Name, p.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil, nil)
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
			Name:     providerName,
			Implicit: true,
		}
		b.providers[providerName] = p
		if err = b.buildProvider(p); err != nil {
			return err
		}
	}
	r.Provider = p
	return nil
}

// buildIgnoreChanges converts the list of ignored properties from Terraform's ignore_changes syntax to Pulumi's
// ignoreChanges syntax.
func buildIgnoreChanges(tfIgnoreChanges []string, schemas Schemas) []string {
	if len(tfIgnoreChanges) == 0 {
		return nil
	}

	ignoreChanges := make([]string, 0, len(tfIgnoreChanges))
	for _, entry := range tfIgnoreChanges {
		// Split the ignore_changes entry on '.'
		elements := strings.Split(entry, ".")

		// If there is one element and that element is "*", ignore all of the top-level properties.
		if len(elements) == 1 && elements[0] == "*" {
			if schemas.TFRes == nil {
				return []string{"*"}
			}

			ignoreChanges = ignoreChanges[:0]
			for k, v := range schemas.TFRes.Schema {
				var p *tfbridge.SchemaInfo
				if schemas.Pulumi != nil {
					p = schemas.Pulumi.Fields[k]
				}
				ignoreChanges = append(ignoreChanges, tfbridge.TerraformToPulumiName(k, v, p, false))
			}
			sort.Strings(ignoreChanges)
			return ignoreChanges
		}

		// Otherwise, convert the entry to an appropriate set of ignores. Note that this process is approximate in the
		// case of Set-typed properties, as we cannot compute set element indices without fully-known configuration and
		// state.
		elemSchemas, path := schemas, ""
		for i, element := range elements {
			// For the last element, we only need a prefix match. Take care of that here.
			if i == len(elements)-1 && elemSchemas.TFRes != nil {
				for k, v := range elemSchemas.TFRes.Schema {
					var p *tfbridge.SchemaInfo
					if schemas.Pulumi != nil {
						p = schemas.Pulumi.Fields[k]
					}
					if strings.HasPrefix(k, element) {
						elementKey := tfbridge.TerraformToPulumiName(k, v, p, false)
						if path == "" {
							ignoreChanges = append(ignoreChanges, elementKey)
						} else {
							ignoreChanges = append(ignoreChanges, path+"."+elementKey)
						}
					}
				}
			} else {
				isListElement := elemSchemas.Type().IsList()
				projectListElement := isListElement && tfbridge.IsMaxItemsOne(elemSchemas.TF, elemSchemas.Pulumi)

				elemSchemas = elemSchemas.PropertySchemas(element)
				if isListElement {
					// If we're projecting the list element, just skip this path element entirely.
					if !projectListElement {
						path = fmt.Sprintf("%s[%s]", path, element)
					}
				} else {
					elementKey := tfbridge.TerraformToPulumiName(element, elemSchemas.TF, elemSchemas.Pulumi, false)
					if path == "" {
						path = elementKey
					} else {
						path += "." + elementKey
					}
				}

				if i == len(elements)-1 {
					ignoreChanges = append(ignoreChanges, path)
				}
			}
		}
	}
	sort.Strings(ignoreChanges)
	return ignoreChanges
}

// buildResource binds a resource's properties (including its count property) and computes its dependency edges.
func (b *builder) buildResource(r *ResourceNode) error {
	if err := b.ensureProvider(r); err != nil {
		return err
	}

	tfName := r.Type + "." + r.Name

	count, countDeps, err := b.bindProperty(tfName+".count", r.Config.RawCount.Value(), Schemas{}, false)
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

	// Bind the resource's properties.
	props, deps, err := b.bindProperties(tfName, r.Config.RawConfig, r.Schemas(), count != nil)
	if err != nil {
		return err
	}

	// Process the `timeouts` property, if any.
	if timeouts, ok := props.Elements["timeouts"]; ok {
		delete(props.Elements, "timeouts")

		timeoutsList, ok := timeouts.(*BoundListProperty)
		if !ok {
			return errors.Errorf("could not parse timeouts for resource %v: timeouts is not a map", tfName)
		}
		if len(timeoutsList.Elements) != 1 {
			return errors.Errorf("could not parse timeouts for resource %v: timeouts is not a map", tfName)
		}
		timeoutsMap, ok := timeoutsList.Elements[0].(*BoundMapProperty)
		if !ok {
			return errors.Errorf("could not parse timeouts for resource %v: timeouts is not a map", tfName)
		}
		r.Timeouts = timeoutsMap
	}

	// Process ignore_changes.
	r.IgnoreChanges = buildIgnoreChanges(r.Config.Lifecycle.IgnoreChanges, r.Schemas())

	// Merge the count dependencies into the overall dependency set and compute the final dependency lists.
	for k := range countDeps {
		deps.add(k)
	}
	allDeps, explicitDeps, err := b.buildDeps(deps, r.Config.DependsOn, []string{r.Config.ProviderFullName()})
	if err != nil {
		return err
	}
	r.Count, r.Properties, r.Deps, r.ExplicitDeps = count, props, allDeps, explicitDeps
	return nil
}

// buildOutput binds an output's value and computes its dependency edges.
func (b *builder) buildOutput(o *OutputNode) error {
	props, deps, err := b.bindProperties(o.Name, o.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, explicitDeps, err := b.buildDeps(deps, o.Config.DependsOn, nil)
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
	props, deps, err := b.bindProperties(l.Name, l.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil, nil)
	contract.Assert(err == nil)

	// In general, a local should have a single property named "value". If this is the case, promote it to the
	// local's value.
	value := BoundNode(props)
	if len(props.Elements) == 1 {
		if v, ok := props.Elements["value"]; ok {
			value = v
		}
	}

	// TODO: locals with object values end up as single-items lists. Sigh.

	l.Value, l.Deps = value, allDeps
	return nil
}

// buildVariable builds a variable's default value (if any). This value must not depend on any other nodes.
func (b *builder) buildVariable(v *VariableNode) error {
	defaultValue, deps, err := b.bindProperty(v.Name+".default", v.Config.Default, Schemas{}, false)
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return errors.Errorf("variables may not depend on other nodes (%v)", v.Name)
	}
	v.DefaultValue = defaultValue
	return nil
}

// ensureBound ensures that the indicated node is bound. If the node is not bound, this method will bind it. If the
// node is currently being bound, this method will return an error due to the circular reference.
func (b *builder) ensureBound(n Node) error {
	// If this node is already bound, we're already done.
	if b.bound[n] {
		return nil
	}

	if b.binding[n] {
		return errors.Errorf("%v either directly or indirectly refers to itself", n.displayName())
	}
	b.binding[n] = true

	var err error
	switch n := n.(type) {
	case *ModuleNode:
		err = b.buildModule(n)
	case *ProviderNode:
		err = b.buildProvider(n)
	case *ResourceNode:
		err = b.buildResource(n)
	case *OutputNode:
		err = b.buildOutput(n)
	case *LocalNode:
		err = b.buildLocal(n)
	case *VariableNode:
		err = b.buildVariable(n)
	}
	b.binding[n], b.bound[n] = false, true
	return err
}

// buildNodes builds the nodes for the given config.
func (b *builder) buildNodes(conf *config.Config) error {
	// Next create our nodes.
	for _, v := range conf.Variables {
		b.variables[v.Name] = &VariableNode{
			Config: v,
			Name:   v.Name,
		}
	}
	for _, p := range conf.ProviderConfigs {
		b.providers[p.FullName()] = &ProviderNode{
			Config: p,
			Name:   p.Name,
			Alias:  p.Alias,
		}
	}
	for _, m := range conf.Modules {
		b.modules[m.Name] = &ModuleNode{
			Config: m,
			Name:   m.Name,
		}
	}
	for _, r := range conf.Resources {
		b.resources[r.Id()] = &ResourceNode{
			Config:       r,
			Name:         r.Name,
			Type:         r.Type,
			IsDataSource: r.Mode == config.DataResourceMode,
		}
	}
	for _, l := range conf.Locals {
		b.locals[l.Name] = &LocalNode{
			Config: l,
			Name:   l.Name,
		}
	}
	for _, o := range conf.Outputs {
		b.outputs[o.Name] = &OutputNode{
			Config: o,
			Name:   o.Name,
		}
	}

	// Now bind each node's properties and compute any dependency edges.
	for _, v := range b.variables {
		if err := b.ensureBound(v); err != nil {
			return err
		}
	}
	for _, p := range b.providers {
		if err := b.ensureBound(p); err != nil {
			return err
		}
	}
	for _, m := range b.modules {
		if err := b.ensureBound(m); err != nil {
			return err
		}
	}
	for _, l := range b.locals {
		if err := b.ensureBound(l); err != nil {
			return err
		}
	}
	for _, r := range b.resources {
		if err := b.ensureBound(r); err != nil {
			return err
		}
	}
	for _, o := range b.outputs {
		if err := b.ensureBound(o); err != nil {
			return err
		}
	}

	return nil
}

// BuildOptions defines the set of optional parameters to `BuildGraph`.
type BuildOptions struct {
	// ProviderInfoSource allows the caller to override the default source for provider schema information, which
	// relies on resource provider plugins.
	ProviderInfoSource ProviderInfoSource
	// AllowMissingProviders allows binding to succeed even if schema information is not available for a provider.
	AllowMissingProviders bool
	// Logger allows the caller to provide a logger for diagnostics. If not provided, the default logger will be used.
	Logger *log.Logger
	// AllowMissingVariables allows binding to succeed even if unknown variables are encountered.
	AllowMissingVariables bool
	// AllowMissingComments allows binding to succeed even if there are errors extracting comments from the source.
	AllowMissingComments bool
}

// BuildGraph analyzes the various entities present in the given module's configuration and constructs the
// corresponding dependency graph. Building the graph involves binding each entity's properties (if any) and
// computing its list of dependency edges.
func BuildGraph(tree *module.Tree, opts *BuildOptions) (*Graph, error) {
	b := newBuilder(opts)

	conf := tree.Config()

	if err := b.buildNodes(conf); err != nil {
		return nil, err
	}

	// Attempt to extract comments from the tree's sources and associate them with the appropriate constructs in the
	// bound graph.
	if err := b.extractComments(conf); err != nil && !opts.AllowMissingComments {
		return nil, err
	}

	// Put the graph together
	return &Graph{
		Tree:      tree,
		Name:      tree.Name(),
		IsRoot:    len(tree.Path()) == 0,
		Path:      conf.Dir,
		Modules:   b.modules,
		Providers: b.providers,
		Resources: b.resources,
		Outputs:   b.outputs,
		Locals:    b.locals,
		Variables: b.variables,
	}, nil
}

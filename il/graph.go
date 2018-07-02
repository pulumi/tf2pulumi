package il

import (
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/workspace"
	"github.com/ugorji/go/codec"
)

// TODO
// - modules
// - provisioners

type Node interface {
	Dependencies() []Node
	sortKey() string
}

type Graph struct {
	Providers map[string]*ProviderNode
	Resources map[string]*ResourceNode
	Outputs   map[string]*OutputNode
	Locals    map[string]*LocalNode
	Variables map[string]*VariableNode
}

type ProviderNode struct {
	Config     *config.ProviderConfig
	Deps       []Node
	Properties *BoundMapProperty
	Info       *tfbridge.ProviderInfo
}

type ResourceNode struct {
	Config       *config.Resource
	Provider     *ProviderNode
	Deps         []Node
	ExplicitDeps []Node
	Count        BoundNode
	Properties   *BoundMapProperty
}

type OutputNode struct {
	Config       *config.Output
	Deps         []Node
	ExplicitDeps []Node
	Value        BoundNode
}

type LocalNode struct {
	Config     *config.Local
	Deps       []Node
	Properties *BoundMapProperty
}
type VariableNode struct {
	Config       *config.Variable
	DefaultValue BoundNode
}

func (p *ProviderNode) Dependencies() []Node {
	return p.Deps
}

func (p *ProviderNode) sortKey() string {
	return "p" + p.Config.Name
}

func (r *ResourceNode) Dependencies() []Node {
	return r.Deps
}

func (r *ResourceNode) Schemas() Schemas {
	switch {
	case r.Provider == nil || r.Provider.Info == nil:
		return Schemas{}
	case r.Config.Mode == config.ManagedResourceMode:
		resInfo := r.Provider.Info.Resources[r.Config.Type]
		return Schemas{
			TFRes: r.Provider.Info.P.ResourcesMap[r.Config.Type],
			Pulumi: &tfbridge.SchemaInfo{Fields: resInfo.Fields},
		}
	default:
		dsInfo := r.Provider.Info.DataSources[r.Config.Type]
		return Schemas{
			TFRes: r.Provider.Info.P.DataSourcesMap[r.Config.Type],
			Pulumi: &tfbridge.SchemaInfo{Fields: dsInfo.Fields},
		}
	}
}

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

func (o *OutputNode) Dependencies() []Node {
	return o.Deps
}

func (o *OutputNode) sortKey() string {
	return "o" + o.Config.Name
}

func (l *LocalNode) Dependencies() []Node {
	return l.Deps
}

func (l *LocalNode) sortKey() string {
	return "l" + l.Config.Name
}

func (v *VariableNode) Dependencies() []Node {
	return nil
}

func (v *VariableNode) sortKey() string {
	return "v" + v.Config.Name
}

type builder struct {
	providers map[string]*ProviderNode
	resources map[string]*ResourceNode
	outputs   map[string]*OutputNode
	locals    map[string]*LocalNode
	variables map[string]*VariableNode
}

func newBuilder() *builder {
	return &builder{
		providers: make(map[string]*ProviderNode),
		resources: make(map[string]*ResourceNode),
		outputs:   make(map[string]*OutputNode),
		locals:    make(map[string]*LocalNode),
		variables: make(map[string]*VariableNode),
	}
}

func (b *builder) getNode(name string) (Node, bool) {
	if p, ok := b.providers[name]; ok {
		return p, true
	}
	if r, ok := b.resources[name]; ok {
		return r, true
	}
	if o, ok := b.outputs[name]; ok {
		return o, true
	}
	if l, ok := b.locals[name]; ok {
		return l, true
	}
	if v, ok := b.variables[name]; ok {
		return v, true
	}
	return nil, false
}

func (b *builder) buildValue(v interface{}, sch Schemas, hasCountIndex bool) (BoundNode, map[Node]struct{}, error) {
	if v == nil {
		return nil, nil, nil
	}

	// Bind the value.
	binder := &propertyBinder{
		builder: b,
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

func (b *builder) buildProperties(raw *config.RawConfig, sch Schemas, hasCountIndex bool) (*BoundMapProperty, map[Node]struct{}, error) {
	v, deps, err := b.buildValue(raw.Raw, sch, hasCountIndex)
	if err != nil {
		return nil, nil, err
	}
	return v.(*BoundMapProperty), deps, nil
}

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

func fixupTFResource(r *schema.Resource) error {
	for _, s := range r.Schema {
		if err := fixupTFSchema(s); err != nil {
			return err
		}
	}
	return nil
}

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

func (b *builder) buildProvider(p *ProviderNode) error {
	info, err := getProviderInfo(p)
	if err != nil {
		return err
	}
	p.Info = info

	props, deps, err := b.buildProperties(p.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil)
	contract.Assert(err == nil)

	p.Properties, p.Deps = props, allDeps
	return nil
}

func (b *builder) ensureProvider(r *ResourceNode) error {
	if r.Provider != nil {
		return nil
	}

	providerName := r.Config.ProviderFullName()
	p, ok := b.providers[providerName]
	if !ok {
		// fake up a provider entry.
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

func (b *builder) buildResource(r *ResourceNode) error {
	b.ensureProvider(r)

	count, countDeps, err := b.buildValue(r.Config.RawCount.Value(), Schemas{}, false)
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

	props, deps, err := b.buildProperties(r.Config.RawConfig, r.Schemas(), count != nil)
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

func (b *builder) buildOutput(o *OutputNode) error {
	props, deps, err := b.buildProperties(o.Config.RawConfig, Schemas{}, false)
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

func (b *builder) buildLocal(l *LocalNode) error {
	props, deps, err := b.buildProperties(l.Config.RawConfig, Schemas{}, false)
	if err != nil {
		return err
	}
	allDeps, _, err := b.buildDeps(deps, nil)
	contract.Assert(err == nil)

	l.Properties, l.Deps = props, allDeps
	return nil
}

func (b *builder) buildVariable(v *VariableNode) error {
	defaultValue, deps, err := b.buildValue(v.Config.Default, Schemas{}, false)
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return errors.Errorf("variables may not depend on other nodes (%v)", v.Config.Name)
	}
	v.DefaultValue = defaultValue
	return nil
}

func BuildGraph(conf *config.Config) (*Graph, error) {
	b := newBuilder()

	// Next create our nodes.
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

	// Now translate each node's properties and connect any dependency edges.
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
		// outputs are sinks; we always deal with them last
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
		// variables are sources; we always deal with them before other nodes.
	}

	// put the graph together
	return &Graph{
		Providers: b.providers,
		Resources: b.resources,
		Outputs:   b.outputs,
		Locals:    b.locals,
		Variables: b.variables,
	}, nil
}

package convert

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/v2/codegen"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2/model"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2/syntax"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/spf13/afero"
	"github.com/zclconf/go-cty/cty"

	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/addrs"
	"github.com/pulumi/tf2pulumi/internal/configs"
)

func parseFile(parser *syntax.Parser, fs afero.Fs, path string) error {
	f, err := fs.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer contract.IgnoreClose(f)

	contract.Assert(path[0] == '/')
	return parser.ParseFile(f, path[1:])
}

// parseTF12 parses a TF12 config.
func parseTF12(opts Options) ([]*syntax.File, hcl.Diagnostics) {
	// Find the config files in the requested directory.
	configs, overrides, diags := configs.NewParser(opts.Root).ConfigDirFiles("/")
	if diags.HasErrors() {
		return nil, diags
	}
	if len(overrides) != 0 {
		return nil, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "modules with overrides are not supported",
			Detail:   "modules with overrides are not supported",
		}}
	}
	for _, config := range configs {
		if strings.HasSuffix(config, ".tf.json") {
			return nil, hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  "JSON configuration is not supported",
				Detail:   "JSON configuration is not supported",
			}}
		}
	}

	// Parse the config.
	parser := syntax.NewParser()
	for _, config := range configs {
		if err := parseFile(parser, opts.Root, config); err != nil {
			return nil, hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("failed to parse file %s", config),
				Detail:   fmt.Sprintf("failed to parse file %s", config),
			}}
		}
	}
	return parser.Files, parser.Diagnostics
}

func convertTF12(files []*syntax.File, opts Options) ([]*syntax.File, *hcl2.Program, hcl.Diagnostics, error) {
	var hcl2Options []model.BindOption
	var pulumiOptions []hcl2.BindOption
	if opts.AllowMissingVariables {
		hcl2Options = append(hcl2Options, model.AllowMissingVariables)
		pulumiOptions = append(pulumiOptions, hcl2.AllowMissingVariables)
	}
	if opts.PluginHost != nil {
		pulumiOptions = append(pulumiOptions, hcl2.PluginHost(opts.PluginHost))
	}
	if opts.PackageCache != nil {
		pulumiOptions = append(pulumiOptions, hcl2.Cache(opts.PackageCache))
	}

	// Bind the files into a module.
	binder := &tf12binder{
		hcl2Options:         hcl2Options,
		pulumiOptions:       pulumiOptions,
		filterResourceNames: opts.FilterResourceNames,
		providerInfo:        il.PluginProviderInfoSource,
		providers:           map[string]*tfbridge.ProviderInfo{},
		binding:             codegen.Set{},
		bound:               codegen.Set{},
		conditionals:        newConditionalAnalyzer(),
		exprToSchemas:       map[model.Expression]il.Schemas{},
		variableToSchemas:   map[model.Definition](func() il.Schemas){},
		tokens:              syntax.NewTokenMapForFiles(files),
		root:                model.NewRootScope(syntax.None),
		providerScope:       model.NewRootScope(syntax.None),
	}

	// Define standard scopes.
	binder.root.DefineScope("data", syntax.None)
	binder.root.DefineScope("var", syntax.None)
	binder.root.DefineScope("local", syntax.None)

	// Define null.
	binder.root.Define("null", &model.Constant{
		Name:          "null",
		ConstantValue: cty.NullVal(cty.DynamicPseudoType),
	})

	// Define builtin functions.
	for name, fn := range tf12builtins {
		binder.root.DefineFunction(name, fn)
	}

	var diagnostics hcl.Diagnostics

	declaredFiles := make([]*file, len(files))
	for i, file := range files {
		f, declareDiags := binder.declareFile(file)
		declaredFiles[i], diagnostics = f, append(diagnostics, declareDiags...)
	}

	for _, file := range declaredFiles {
		bindDiags := binder.bindFile(file)
		diagnostics = append(diagnostics, bindDiags...)
	}

	// Convert the module into a Pulumi HCL2 program.
	assignNames(declaredFiles)
	for _, file := range declaredFiles {
		genDiags := binder.genFile(file)
		diagnostics = append(diagnostics, genDiags...)
	}

	pulumiParser := syntax.NewParser()
	for _, file := range declaredFiles {
		contents := file.output.String()

		err := pulumiParser.ParseFile(file.output, file.syntax.Name+".pp")
		contract.AssertNoError(err)
		file.output.Reset()

		if pulumiParser.Diagnostics.HasErrors() {
			log.Printf("%v", contents)
			log.Printf("%v", diagnostics)
			log.Printf("%v", pulumiParser.Diagnostics)
			contract.Fail()
		}
	}

	program, programDiags, err := hcl2.BindProgram(pulumiParser.Files, pulumiOptions...)
	diagnostics = append(diagnostics, programDiags...)

	return pulumiParser.Files, program, diagnostics, err
}

type tf12binder struct {
	pulumiOptions       []hcl2.BindOption
	hcl2Options         []model.BindOption
	filterResourceNames bool
	providerInfo        il.ProviderInfoSource

	providers map[string]*tfbridge.ProviderInfo

	binding codegen.Set
	bound   codegen.Set

	conditionals      *conditionalAnalyzer
	exprToSchemas     map[model.Expression]il.Schemas
	variableToSchemas map[model.Definition](func() il.Schemas)
	tokens            syntax.TokenMap
	root              *model.Scope
	providerScope     *model.Scope
}

type tf12Node interface {
	SyntaxNode() hclsyntax.Node
}

type file struct {
	syntax *syntax.File

	nodes []tf12Node

	output *bytes.Buffer
}

type bodyItem struct {
	syntax hclsyntax.Node
	item   model.BodyItem
}

func (b *bodyItem) SyntaxNode() hclsyntax.Node {
	return b.syntax
}

type variable struct {
	syntax *hclsyntax.Block

	name          string
	pulumiName    string
	terraformType model.Type

	block *model.Block
}

func (v *variable) SyntaxNode() hclsyntax.Node {
	return v.syntax
}

func (v *variable) Traverse(traverser hcl.Traverser) (model.Traversable, hcl.Diagnostics) {
	return v.terraformType.Traverse(traverser)
}

func (v *variable) Type() model.Type {
	return v.terraformType
}

type provider struct {
	syntax *hclsyntax.Block

	alias      string
	pluginName string
	pulumiName string

	block *model.Block
}

func (p *provider) SyntaxNode() hclsyntax.Node {
	return p.syntax
}

func (p *provider) Traverse(traverser hcl.Traverser) (model.Traversable, hcl.Diagnostics) {
	rng := traverser.SourceRange()
	return nil, hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  "providers are not traversable",
		Subject:  &rng,
	}}
}

func (p *provider) Type() model.Type {
	return model.DynamicType
}

type local struct {
	syntax *hclsyntax.Attribute

	name          string
	pulumiName    string
	schemas       il.Schemas
	terraformType model.Type

	attribute *model.Attribute
}

func (l *local) SyntaxNode() hclsyntax.Node {
	return l.syntax
}

func (l *local) Traverse(traverser hcl.Traverser) (model.Traversable, hcl.Diagnostics) {
	return l.terraformType.Traverse(traverser)
}

func (l *local) Type() model.Type {
	return l.terraformType
}

type output struct {
	syntax *hclsyntax.Block

	name       string
	pulumiName string

	block *model.Block
}

func (o *output) SyntaxNode() hclsyntax.Node {
	return o.syntax
}

// nolint: structcheck, unused
type module struct {
	syntax *hclsyntax.Block

	name          string
	pulumiName    string
	pulumiType    model.Type
	terraformType model.Type

	block *model.Block
}

func (m *module) SyntaxNode() hclsyntax.Node {
	return m.syntax
}

func (m *module) Traverse(traverser hcl.Traverser) (model.Traversable, hcl.Diagnostics) {
	return m.terraformType.Traverse(traverser)
}

type resource struct {
	syntax *hclsyntax.Block

	isDataSource  bool
	name          string
	pulumiName    string
	typeName      string
	token         string
	schemas       il.Schemas
	terraformType model.Type
	variableType  model.Type
	rangeVariable *model.Variable
	isCounted     bool
	isConditional bool

	block *model.Block
}

func (r *resource) SyntaxNode() hclsyntax.Node {
	return r.syntax
}

func (r *resource) Traverse(traverser hcl.Traverser) (model.Traversable, hcl.Diagnostics) {
	return r.variableType.Traverse(traverser)
}

func (r *resource) Type() model.Type {
	return r.variableType
}

func (b *tf12binder) declareFile(input *syntax.File) (*file, hcl.Diagnostics) {
	var diagnostics hcl.Diagnostics

	file := &file{syntax: input, output: &bytes.Buffer{}}
	for _, item := range model.SourceOrderBody(input.Body) {
		switch item := item.(type) {
		case *hclsyntax.Block:
			switch item.Type {
			case "variable":
				v := &variable{
					syntax: item,
					name:   item.Labels[0],
				}
				scopeDef, _ := b.root.BindReference("var")
				scopeDef.(*model.Scope).Define(v.name, v)
				file.nodes = append(file.nodes, v)
			case "provider":
				p := &provider{
					syntax:     item,
					pluginName: item.Labels[0], // TODO(pdg): properly map plugin names
				}

				if alias, ok := item.Body.Attributes["alias"]; ok {
					boundAlias, _ := model.BindAttribute(alias, nil, b.tokens, b.hcl2Options...)
					if t, ok := boundAlias.Value.(*model.TemplateExpression); ok && len(t.Parts) == 1 {
						if lit, ok := t.Parts[0].(*model.LiteralValueExpression); ok {
							p.alias = lit.Value.AsString()
						}
					}
				}

				if p.alias != "" {
					scopeDef, ok := b.providerScope.BindReference(item.Labels[0])
					if !ok {
						scopeDef, _ = b.providerScope.DefineScope(item.Labels[0], syntax.None)
					}
					scopeDef.(*model.Scope).Define(p.alias, p)
				}

				file.nodes = append(file.nodes, p)
			case "locals":
				for _, item := range model.SourceOrderBody(item.Body) {
					if item, ok := item.(*hclsyntax.Attribute); ok {
						l := &local{
							syntax: item,
							name:   item.Name,
						}
						scopeDef, _ := b.root.BindReference("local")
						scopeDef.(*model.Scope).Define(l.name, l)
						file.nodes = append(file.nodes, l)
					}
				}
			case "output":
				o := &output{
					syntax: item,
					name:   item.Labels[0],
				}
				file.nodes = append(file.nodes, o)
				//			case "module":
				//				// TODO(pdg): module instances
			case "resource", "data":
				isDataSource := item.Type == "data"

				mode := addrs.ManagedResourceMode
				if isDataSource {
					mode = addrs.DataResourceMode
				}

				addr := addrs.Resource{Mode: mode, Type: item.Labels[0], Name: item.Labels[1]}
				token, schemas, terraformType, typeDiags := b.resourceType(addr, item.LabelRanges[0])
				diagnostics = append(diagnostics, typeDiags...)

				variableType := terraformType
				_, hasCount := item.Body.Attributes["count"]
				_, hasForEach := item.Body.Attributes["for_each"]
				if hasCount || hasForEach {
					variableType = model.NewListType(terraformType)
				}

				r := &resource{
					syntax:        item,
					isDataSource:  isDataSource,
					name:          addr.Name,
					typeName:      addr.Type,
					token:         token,
					schemas:       schemas,
					terraformType: terraformType,
					variableType:  variableType,
				}

				rootScope := b.root
				if isDataSource {
					s, _ := b.root.BindReference("data")
					rootScope = s.(*model.Scope)
				}

				scopeDef, ok := rootScope.BindReference(addr.Type)
				if !ok {
					scopeDef, _ = rootScope.DefineScope(addr.Type, syntax.None)
				}

				scopeDef.(*model.Scope).Define(addr.Name, r)
				file.nodes = append(file.nodes, r)
			default:
				file.nodes = append(file.nodes, &bodyItem{syntax: item})
			}
		default:
			file.nodes = append(file.nodes, &bodyItem{syntax: item})
		}
	}
	return file, diagnostics
}

func (b *tf12binder) bindNode(node tf12Node) hcl.Diagnostics {
	if b.bound.Has(node) {
		return nil
	}
	if b.binding.Has(node) {
		// TODO(pdg): print trace
		rng := node.SyntaxNode().Range()
		return hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "circular reference",
			Subject:  &rng,
		}}
	}
	b.binding.Add(node)

	// Collect and bind the node's dependencies.
	depSet := codegen.Set{}
	var deps []tf12Node
	diagnostics := hclsyntax.VisitAll(node.SyntaxNode(), func(n hclsyntax.Node) hcl.Diagnostics {
		x, ok := n.(*hclsyntax.ScopeTraversalExpr)
		if !ok {
			return nil
		}

		// Missing reference errors will be issued during expression binding.
		definition, _ := b.root.BindReference(x.Traversal.RootName())
		for _, traverser := range x.Traversal[1:] {
			scope, ok := definition.(*model.Scope)
			if !ok {
				break
			}
			key, _ := model.GetTraverserKey(traverser)
			if key.Type() != cty.String {
				break
			}
			definition, _ = scope.BindReference(key.AsString())
		}
		if node, ok := definition.(tf12Node); ok && !depSet.Has(node) {
			depSet.Add(node)
			deps = append(deps, node)
		}
		return nil
	})
	contract.Assert(len(diagnostics) == 0)

	sort.Slice(deps, func(i, j int) bool {
		return model.SourceOrderLess(deps[i].SyntaxNode().Range(), deps[j].SyntaxNode().Range())
	})
	for _, dep := range deps {
		diags := b.bindNode(dep)
		diagnostics = append(diagnostics, diags...)
	}

	switch node := node.(type) {
	case *bodyItem:
		diags := b.bindBodyItem(node)
		diagnostics = append(diagnostics, diags...)
	case *variable:
		diags := b.bindVariable(node)
		diagnostics = append(diagnostics, diags...)
	case *provider:
		diags := b.bindProvider(node)
		diagnostics = append(diagnostics, diags...)
	case *local:
		diags := b.bindLocal(node)
		diagnostics = append(diagnostics, diags...)
	case *output:
		diags := b.bindOutput(node)
		diagnostics = append(diagnostics, diags...)
	case *module:
		diags := b.bindModule(node)
		diagnostics = append(diagnostics, diags...)
	case *resource:
		diags := b.bindResource(node)
		diagnostics = append(diagnostics, diags...)
	}

	b.bound.Add(node)
	return diagnostics
}

func (b *tf12binder) bindFile(file *file) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics
	for _, node := range file.nodes {
		diags := b.bindNode(node)
		diagnostics = append(diagnostics, diags...)
	}
	return diagnostics
}

func (b *tf12binder) genFile(file *file) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics
	for _, node := range file.nodes {
		switch node := node.(type) {
		case *bodyItem:
			diags := b.genBodyItem(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *variable:
			diags := b.genVariable(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *provider:
			diags := b.genProvider(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *local:
			diags := b.genLocal(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *output:
			diags := b.genOutput(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *module:
			diags := b.genModule(file.output, node)
			diagnostics = append(diagnostics, diags...)
		case *resource:
			diags := b.genResource(file.output, node)
			diagnostics = append(diagnostics, diags...)
		}
	}
	return diagnostics
}

func (b *tf12binder) getTraversalSchemas(traversal hcl.Traversal, schemas il.Schemas) il.Schemas {
	for _, traverser := range traversal {
		switch traverser := traverser.(type) {
		case hcl.TraverseAttr:
			schemas = schemas.PropertySchemas(traverser.Name)
		case hcl.TraverseIndex:
			schemas = schemas.ElemSchemas()
		default:
			contract.Failf("unexpected traverser of type %T (%v)", traverser, traverser.SourceRange())
		}
	}
	return schemas
}

func (b *tf12binder) annotateExpressionsWithSchemas(item model.BodyItem) {
	_, diags := model.VisitBodyItem(item, model.BodyItemIdentityVisitor,
		func(item model.BodyItem) (model.BodyItem, hcl.Diagnostics) {
			if item, ok := item.(*model.Attribute); ok {
				_, diags := model.VisitExpression(item.Value,
					func(x model.Expression) (model.Expression, hcl.Diagnostics) {
						switch x := x.(type) {
						case *model.ForExpression:
							// TODO(pdg): implement
						case *model.SplatExpression:
							b.variableToSchemas[x.Item] = func() il.Schemas {
								return b.exprToSchemas[x.Source]
							}
						}
						return x, nil
					},
					func(x model.Expression) (model.Expression, hcl.Diagnostics) {
						switch x := x.(type) {
						case *model.ConditionalExpression:
							// TODO(pdg): implement
						case *model.ForExpression:
							// TODO(pdg): implement
						case *model.IndexExpression:
							if s, ok := b.exprToSchemas[x.Collection]; ok {
								// TODO(pdg): proper handling of object- and tuple-typed collections
								b.exprToSchemas[x] = s.ElemSchemas()
							}
						case *model.ObjectConsExpression:
							// TODO(pdg): implement
						case *model.RelativeTraversalExpression:
							if s, ok := b.exprToSchemas[x.Source]; ok {
								b.exprToSchemas[x] = b.getTraversalSchemas(x.Traversal, s)
							}
						case *model.SplatExpression:
							if s, ok := b.exprToSchemas[x.Each]; ok {
								var schemas il.Schemas
								switch {
								case s.TF != nil:
									schemas.TF = &schema.Schema{Type: schema.TypeList, Elem: s.TF}
								case s.TFRes != nil:
									schemas.TF = &schema.Schema{Type: schema.TypeList, Elem: s.TFRes}
								}

								if s.Pulumi != nil {
									schemas.Pulumi = &tfbridge.SchemaInfo{Elem: s.Pulumi}
								}

								b.exprToSchemas[x] = schemas
							}
						case *model.TupleConsExpression:
							// TODO(pdg): imeplement
						case *model.ScopeTraversalExpression:
							traversal := x.Traversal
							contract.Assertf(len(traversal) == len(x.Parts), "%v: %v != %v", x, len(traversal), len(x.Parts))

							var schemas il.Schemas
							for _, p := range x.Parts {
								traversal = traversal[1:]
								switch p := p.(type) {
								case *local:
									schemas = p.schemas
								case *resource:
									schemas = p.schemas
								case *model.Variable:
									fn, ok := b.variableToSchemas[p]
									if !ok {
										return x, nil
									}
									schemas = fn()
								default:
									continue
								}
								break
							}

							b.exprToSchemas[x] = b.getTraversalSchemas(traversal, schemas)
						}
						return x, nil
					})
				contract.Assert(len(diags) == 0)
			}
			return item, nil
		})
	contract.Assert(len(diags) == 0)
}

func (b *tf12binder) bindBodyItem(item *bodyItem) hcl.Diagnostics {
	var result model.BodyItem
	var diagnostics hcl.Diagnostics
	switch item := item.syntax.(type) {
	case *hclsyntax.Attribute:
		result, diagnostics = model.BindAttribute(item, b.root, b.tokens, b.hcl2Options...)
	case *hclsyntax.Block:
		result, diagnostics = model.BindBlock(item, model.StaticScope(b.root), b.tokens, b.hcl2Options...)
	}
	item.item = result
	return diagnostics
}

type variableScopes int

func (variableScopes) GetScopesForBlock(block *hclsyntax.Block) (model.Scopes, hcl.Diagnostics) {
	return model.StaticScope(nil), nil
}

func (variableScopes) GetScopeForAttribute(attribute *hclsyntax.Attribute) (*model.Scope, hcl.Diagnostics) {
	if attribute.Name == "type" {
		return model.TypeScope, nil
	}
	return nil, nil
}

func (b *tf12binder) bindVariable(v *variable) hcl.Diagnostics {
	block, diagnostics := model.BindBlock(v.syntax, variableScopes(0), b.tokens, b.hcl2Options...)
	b.annotateExpressionsWithSchemas(block)

	variableType := model.Type(model.DynamicType)
	if typeDecl, hasType := block.Body.Attribute("type"); hasType {
		variableType = typeDecl.Value.Type()
	} else if defaultDecl, hasDefault := block.Body.Attribute("default"); hasDefault {
		variableType = defaultDecl.Value.Type()
	}

	v.terraformType, v.block = variableType, block
	return diagnostics
}

func (b *tf12binder) bindProvider(p *provider) hcl.Diagnostics {
	block, diagnostics := model.BindBlock(p.syntax, model.StaticScope(b.root), b.tokens, b.hcl2Options...)
	b.annotateExpressionsWithSchemas(block)

	p.block = block
	return diagnostics
}

func (b *tf12binder) bindLocal(l *local) hcl.Diagnostics {
	attribute, diagnostics := model.BindAttribute(l.syntax, b.root, b.tokens, b.hcl2Options...)
	b.annotateExpressionsWithSchemas(attribute)
	l.schemas = b.exprToSchemas[attribute.Value]
	l.terraformType, l.attribute = attribute.Value.Type(), attribute
	return diagnostics
}

func (b *tf12binder) bindOutput(o *output) hcl.Diagnostics {
	block, diagnostics := model.BindBlock(o.syntax, model.StaticScope(b.root), b.tokens, b.hcl2Options...)
	b.annotateExpressionsWithSchemas(block)
	o.block = block
	return diagnostics
}

func (b *tf12binder) bindModule(m *module) hcl.Diagnostics {
	return nil
}

type resourceScopes struct {
	isDataSource   bool
	root           *model.Scope
	providers      *model.Scope
	attributeScope *model.Scope
	terraformType  model.Type
}

func (s *resourceScopes) GetScopesForBlock(block *hclsyntax.Block) (model.Scopes, hcl.Diagnostics) {
	if s.isDataSource && block.Type == "lifecycle" {
		return &lifecycleScopes{terraformType: s.terraformType}, nil
	}
	return model.StaticScope(s.root), nil
}

func (s *resourceScopes) GetScopeForAttribute(attribute *hclsyntax.Attribute) (*model.Scope, hcl.Diagnostics) {
	switch attribute.Name {
	case "depends_on", "count", "for_each":
		return s.root, nil
	case "provider":
		return s.providers, nil
	}
	return s.attributeScope, nil
}

type lifecycleScopes struct {
	terraformType model.Type
}

func (s *lifecycleScopes) GetScopesForBlock(block *hclsyntax.Block) (model.Scopes, hcl.Diagnostics) {
	return s, nil
}

func (s *lifecycleScopes) GetScopeForAttribute(attribute *hclsyntax.Attribute) (*model.Scope, hcl.Diagnostics) {
	if attribute.Name == "ignore_changes" {
		if _, isTraversal := attribute.Expr.(*hclsyntax.ScopeTraversalExpr); isTraversal {
			scope := model.NewRootScope(syntax.None)
			scope.Define("all", model.Keyword("all"))
			return scope, nil
		}

		if obj, ok := s.terraformType.(*model.ObjectType); ok {
			scope := model.NewRootScope(syntax.None)
			for prop, typ := range obj.Properties {
				if prop != "id" {
					scope.Define(prop, typ)
				}
			}
			return scope, nil
		}
	}
	return nil, nil
}

func (b *tf12binder) bindResource(r *resource) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics

	var rangeDef hclsyntax.Node
	if count, hasCount := r.syntax.Body.Attributes["count"]; hasCount {
		rangeDef = count
		r.rangeVariable = &model.Variable{
			Name: "count",
			VariableType: model.NewObjectType(map[string]model.Type{
				"index": model.NumberType,
			}),
		}
		b.variableToSchemas[r.rangeVariable] = func() il.Schemas {
			return il.Schemas{
				Pulumi: &tfbridge.SchemaInfo{
					Fields: map[string]*tfbridge.SchemaInfo{
						"index": {Name: "value"},
					},
				},
			}
		}
	} else if forEach, hasForEach := r.syntax.Body.Attributes["for_each"]; hasForEach {
		forEachExpr, _ := model.BindExpression(forEach.Expr, b.root, b.tokens, b.hcl2Options...)
		keyType, valueType, diags := model.GetCollectionTypes(forEachExpr.Type(), forEach.Expr.Range())
		diagnostics = append(diagnostics, diags...)

		rangeDef = forEach
		r.rangeVariable = &model.Variable{
			Name: "each",
			VariableType: model.NewObjectType(map[string]model.Type{
				"key":   keyType,
				"value": valueType,
			}),
		}
	}

	attributeScope := b.root
	if r.rangeVariable != nil {
		attributeScope = b.root.Push(rangeDef)
		attributeScope.Define(r.rangeVariable.Name, r.rangeVariable)
	}
	scopes := &resourceScopes{
		isDataSource:   r.isDataSource,
		root:           b.root,
		attributeScope: attributeScope,
		providers:      b.providerScope,
		terraformType:  r.terraformType,
	}

	block, diags := model.BindBlock(r.syntax, scopes, b.tokens, b.hcl2Options...)
	diagnostics = append(diagnostics, diags...)

	if r.rangeVariable != nil {
		var rangeExpr model.Expression
		if count, hasCount := block.Body.Attribute("count"); hasCount {
			r.isConditional = b.conditionals.isConditionalValue(count.Value)
			r.isCounted = !r.isConditional

			rangeExpr = count.Value
			b.annotateExpressionsWithSchemas(count)
		} else {
			forEach, _ := block.Body.Attribute("for_each")
			rangeExpr = forEach.Value
			b.annotateExpressionsWithSchemas(forEach)
		}
		if s, ok := b.exprToSchemas[rangeExpr]; ok {
			b.variableToSchemas[r.rangeVariable] = func() il.Schemas {
				return s
			}
		}
	}
	b.annotateExpressionsWithSchemas(block)

	r.block = block
	return diagnostics
}

func (b *tf12binder) genBodyItem(w io.Writer, item *bodyItem) hcl.Diagnostics {
	_, err := fmt.Fprintf(w, "%v", item.item)
	contract.IgnoreError(err)
	return nil
}

func (b *tf12binder) genVariable(w io.Writer, v *variable) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics

	var bodyItems []model.BodyItem
	if defaultValue, ok := v.block.Body.Attribute("default"); ok {
		dv, diags := b.rewriteExpression(defaultValue.Value, nil)
		defaultValue.Value, diagnostics = dv, append(diagnostics, diags...)

		bodyItems = []model.BodyItem{defaultValue}
	}
	v.block.Body.Items = bodyItems

	v.block.Type = "config"
	v.block.Labels[0] = v.pulumiName
	if v.terraformType != model.DynamicType {
		v.block.Labels = append(v.block.Labels, fmt.Sprintf("%v", v.terraformType))
	}

	_, err := fmt.Fprintf(w, "%v", v.block)
	contract.IgnoreError(err)
	return diagnostics
}

func (b *tf12binder) genProvider(w io.Writer, p *provider) hcl.Diagnostics {
	if p.alias == "" {
		rng := p.syntax.Range()
		return hcl.Diagnostics{{
			Severity: hcl.DiagWarning,
			Summary:  "default provider configuration is not supported",
			Subject:  &rng,
		}}
	}

	token, schemas, terraformType, diagnostics := b.providerType(p.syntax.Labels[0], p.syntax.LabelRanges[0])
	if diagnostics != nil {
		return diagnostics
	}

	bodyItems := make([]model.BodyItem, 0, len(p.block.Body.Items)-1)
	for _, item := range p.block.Body.Items {
		if item, ok := item.(*model.Attribute); ok && item.Name == "alias" {
			continue
		}
		bodyItems = append(bodyItems, item)
	}
	p.block.Body.Items = bodyItems
	p.block.Type = "resource"

	return b.genResource(w, &resource{
		syntax:        p.syntax,
		name:          p.alias,
		pulumiName:    p.pulumiName,
		token:         token,
		schemas:       schemas,
		terraformType: terraformType,
		block:         p.block,
	})
}

func (b *tf12binder) genLocal(w io.Writer, l *local) hcl.Diagnostics {
	l.attribute.Name = l.pulumiName

	v, diagnostics := b.rewriteExpression(l.attribute.Value, nil)
	l.attribute.Value = v

	_, err := fmt.Fprintf(w, "%v", l.attribute)
	contract.IgnoreError(err)
	return diagnostics
}

func (b *tf12binder) genOutput(w io.Writer, o *output) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics
	var bodyItems []model.BodyItem
	if value, ok := o.block.Body.Attribute("value"); ok {
		v, diags := b.rewriteExpression(value.Value, nil)
		value.Value, diagnostics = v, append(diagnostics, diags...)

		bodyItems = []model.BodyItem{value}
	}
	o.block.Body.Items = bodyItems

	o.block.Labels[0] = o.pulumiName

	_, err := fmt.Fprintf(w, "%v", o.block)
	contract.IgnoreError(err)
	return diagnostics
}

func (b *tf12binder) genModule(w io.Writer, m *module) hcl.Diagnostics {
	// TODO(pdg): implement me
	return nil
}

type blockInfo struct {
	name           string
	schemas        il.Schemas
	elidedFields   codegen.StringSet
	groupedTypes   map[string][]*model.Block
	rewrittenTypes codegen.StringSet
}

type resourceRewriter struct {
	binder   *tf12binder
	resource *resource
	stack    []*blockInfo
	options  *model.Block
}

func (rr *resourceRewriter) schemas() il.Schemas {
	return rr.stack[len(rr.stack)-1].schemas
}

func (rr *resourceRewriter) isElidedField(name string) bool {
	return rr.stack[len(rr.stack)-2].elidedFields.Has(name)
}

func (rr *resourceRewriter) group(name string) []*model.Block {
	return rr.stack[len(rr.stack)-1].groupedTypes[name]
}

func (rr *resourceRewriter) isRewritten(name string) bool {
	return rr.stack[len(rr.stack)-1].rewrittenTypes.Has(name)
}

func (rr *resourceRewriter) markRewritten(name string) {
	rr.stack[len(rr.stack)-1].rewrittenTypes.Add(name)
}

func (rr *resourceRewriter) push(name string, block bool) *blockInfo {
	schemas := rr.resource.schemas
	if len(rr.stack) > 0 {
		schemas = rr.schemas().PropertySchemas(name)
	}
	if block && schemas.Type().IsList() {
		schemas = schemas.ElemSchemas()
	}
	info := &blockInfo{
		name:           name,
		schemas:        schemas,
		elidedFields:   codegen.StringSet{},
		groupedTypes:   map[string][]*model.Block{},
		rewrittenTypes: codegen.StringSet{},
	}
	rr.stack = append(rr.stack, info)
	return info
}

func (rr *resourceRewriter) pushElem() *blockInfo {
	contract.Assert(len(rr.stack) > 0)
	schemas := rr.schemas().ElemSchemas()
	info := &blockInfo{
		name:           rr.stack[len(rr.stack)-1].name,
		schemas:        schemas,
		elidedFields:   codegen.StringSet{},
		groupedTypes:   map[string][]*model.Block{},
		rewrittenTypes: codegen.StringSet{},
	}
	rr.stack = append(rr.stack, info)
	return info
}

func (rr *resourceRewriter) pop() {
	rr.stack = rr.stack[:len(rr.stack)-1]
}

//nolint: unused
func (rr *resourceRewriter) dumpStack() {
	fmt.Printf("[")
	for i, info := range rr.stack {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%v (%v)", info.name, len(info.groupedTypes))
	}
	fmt.Printf("]\n")
}

func (rr *resourceRewriter) appendOption(item *model.Attribute) model.BodyItem {
	var result model.BodyItem
	if rr.options == nil {
		rr.options = &model.Block{
			Type: "options",
			Body: &model.Body{},
		}
		result = rr.options
	}
	if item.Tokens != nil && rr.options.Tokens == nil {
		rr.options.Tokens = syntax.NewBlockTokens("options")
		rr.options.Tokens.OpenBrace.LeadingTrivia = item.Tokens.Name.LeadingTrivia.LeadingWhitespace()
	}
	rr.options.Body.Items = append(rr.options.Body.Items, item)
	return result
}

func (rr *resourceRewriter) terraformToPulumiName(tfName string) string {
	schemas := rr.schemas()
	if schemas.Pulumi != nil && schemas.Pulumi.Name != "" {
		return schemas.Pulumi.Name
	}
	return tfbridge.TerraformToPulumiName(tfName, schemas.TF, schemas.Pulumi, false)
}

func (rr *resourceRewriter) enterBodyItem(item model.BodyItem) (model.BodyItem, hcl.Diagnostics) {
	var diagnostics hcl.Diagnostics

	switch item := item.(type) {
	case *model.Attribute:
		rr.push(item.Name, false)
	case *model.Block:
		info := rr.push(item.Type, true)

		for _, item := range item.Body.Items {
			switch item := item.(type) {
			case *model.Attribute:
				propSch := rr.schemas().PropertySchemas(item.Name)
				if propSch.Pulumi != nil {
					if propSch.Pulumi.Asset != nil {
						info.elidedFields.Add(propSch.Pulumi.Asset.HashField)
					}
					if def := propSch.Pulumi.Default; rr.binder.filterResourceNames && def != nil && def.AutoNamed {
						info.elidedFields.Add(item.Name)
					}
				}
			case *model.Block:
				info.groupedTypes[item.Type] = append(info.groupedTypes[item.Type], item)
			}
		}
	}

	return item, diagnostics
}

func (rr *resourceRewriter) rewriteObjectKeys(expr model.Expression) {
	switch expr := expr.(type) {
	case *model.ObjectConsExpression:
		schemas := rr.schemas()
		useExactKeys := schemas.TF != nil && schemas.TF.Type == schema.TypeMap

		for _, item := range expr.Items {
			// Ignore non-literal keys
			keyVal := ""
			if !useExactKeys {
				if key, ok := item.Key.(*model.LiteralValueExpression); ok && key.Value.Type().Equals(cty.String) {
					keyVal = rr.terraformToPulumiName(key.Value.AsString())
					key.Value = cty.StringVal(keyVal)
				}
			}

			rr.push(keyVal, false)
			rr.rewriteObjectKeys(item.Value)
			rr.pop()
		}
	case *model.TupleConsExpression:
		rr.pushElem()
		for _, element := range expr.Expressions {
			rr.rewriteObjectKeys(element)
		}
		rr.pop()
	}
}

func (rr *resourceRewriter) rewriteBlockAsObjectCons(block *model.Block) *model.ObjectConsExpression {
	tokens := syntax.NewObjectConsTokens(len(block.Body.Items))
	if block.Tokens != nil {
		tokens.OpenBrace = block.Tokens.OpenBrace
		tokens.CloseBrace = block.Tokens.CloseBrace
	}

	var leadingTrivia syntax.TriviaList
	for _, l := range block.Tokens.GetLabels(block.Labels) {
		leadingTrivia = append(leadingTrivia, l.AllTrivia().CollapseWhitespace()...)
	}
	tokens.OpenBrace.LeadingTrivia = append(leadingTrivia, tokens.OpenBrace.LeadingTrivia.CollapseWhitespace()...)

	objectCons := &model.ObjectConsExpression{Tokens: tokens}
	for i, item := range block.Body.Items {
		attr, ok := item.(*model.Attribute)
		contract.Assert(ok)

		objectCons.Items = append(objectCons.Items, model.ObjectConsItem{
			Key: &model.LiteralValueExpression{
				Tokens: &syntax.LiteralValueTokens{
					Value: []syntax.Token{attr.Tokens.GetName(attr.Name)},
				},
				Value: cty.StringVal(attr.Name),
			},
			Value: attr.Value,
		})

		if attr.Tokens != nil {
			tokens.Items[i].Equals = attr.Tokens.Equals
		}
		if i < len(block.Body.Items)-1 && attr.HasTrailingTrivia() {
			tokens.Items[i].Comma.TrailingTrivia = attr.Value.GetTrailingTrivia()
			attr.Value.SetTrailingTrivia(nil)
		}
	}

	return objectCons
}

func (rr *resourceRewriter) rewriteBodyItem(item model.BodyItem) (model.BodyItem, hcl.Diagnostics) {
	defer rr.pop()

	var diagnostics hcl.Diagnostics

	switch item := item.(type) {
	case *model.Attribute:
		if rr.isElidedField(item.Name) {
			// TODO: transfer trivia
			return nil, nil
		}

		rr.rewriteObjectKeys(item.Value)
		value, diags := rr.binder.rewriteExpression(item.Value, rr.resource)
		diagnostics = append(diagnostics, diags...)

		if len(rr.stack) == 2 {
			switch item.Name {
			case "depends_on":
				item.Name = "dependsOn"
				return rr.appendOption(item), nil
			case "count":
				item.Name = "range"
				return rr.appendOption(item), nil
			case "for_each":
				item.Name = "range"
				return rr.appendOption(item), nil
			case "provider":
				item.Name = "provider"
				return rr.appendOption(item), nil
			}
		}

		propSch := rr.schemas()
		if propSch.Pulumi != nil && propSch.Pulumi.Asset != nil {
			asset := propSch.Pulumi.Asset

			var call *model.FunctionCallExpression
			if asset.Kind == tfbridge.FileArchive || asset.Kind == tfbridge.BytesArchive {
				call = &model.FunctionCallExpression{
					Name: "fileArchive",
					Args: []model.Expression{value},
				}
			} else {
				call = &model.FunctionCallExpression{
					Name: "fileAsset",
					Args: []model.Expression{value},
				}
			}
			call.Tokens = syntax.NewFunctionCallTokens(call.Name, len(call.Args))

			if value.HasLeadingTrivia() {
				call.SetLeadingTrivia(value.GetLeadingTrivia())
				value.SetLeadingTrivia(nil)
			}
			if value.HasTrailingTrivia() {
				call.SetTrailingTrivia(value.GetTrailingTrivia())
				value.SetTrailingTrivia(nil)
			}

			value = call
		}

		item.Name, item.Value = rr.terraformToPulumiName(item.Name), value
	case *model.Block:
		if len(rr.stack) == 2 {
			switch item.Type {
			case "lifecycle":
				var result model.BodyItem
				preventDestroy, ok := item.Body.Attribute("prevent_destroy")
				if ok {
					preventDestroy.Name = "protect"
					result = rr.appendOption(preventDestroy)
				}
				ignoreChanges, ok := item.Body.Attribute("ignore_changes")
				if ok {
					ignoreChanges.Name = "ignoreChanges"
					if options := rr.appendOption(ignoreChanges); options != nil {
						result = options
					}
				}
				return result, nil
			case "provisioner", "connection":
				rng := item.Syntax.TypeRange
				return item, hcl.Diagnostics{{
					Severity: hcl.DiagError,
					Summary:  "tf2pulumi does not support provisioners",
					Subject:  &rng,
				}}
			}
		}

		items := make([]model.BodyItem, 0, len(item.Body.Items))
		for _, item := range item.Body.Items {
			block, ok := item.(*model.Block)
			if !ok || len(rr.stack) == 1 && block.Type == "options" {
				items = append(items, item)
				continue
			}
			if rr.isRewritten(block.Type) {
				continue
			}

			rr.markRewritten(block.Type)

			group := rr.group(block.Type)
			objects := make([]model.Expression, len(group))
			for i, block := range group {
				objects[i] = rr.rewriteBlockAsObjectCons(block)
			}

			propSch := rr.schemas().PropertySchemas(block.Type)
			_, isList := propSch.ModelType().(*model.ListType)
			projectListElement := isList && tfbridge.IsMaxItemsOne(propSch.TF, propSch.Pulumi)

			tokens := syntax.NewAttributeTokens(block.Type)

			var value model.Expression
			if !projectListElement || len(objects) > 1 {
				if block.Tokens != nil {
					tokens.Name.LeadingTrivia = block.Tokens.Type.LeadingTrivia
				}

				tokens := syntax.NewTupleConsTokens(len(objects))
				for i, o := range objects {
					if i > 0 && group[i].Tokens != nil {
						leading := append(group[i].Tokens.Type.AllTrivia(), o.GetLeadingTrivia().CollapseWhitespace()...)
						o.SetLeadingTrivia(leading)
					}
					if i == len(objects)-1 {
						tokens.CloseBracket.TrailingTrivia = o.GetTrailingTrivia()
					} else {
						tokens.Commas[i].TrailingTrivia = o.GetTrailingTrivia()
					}
					o.SetTrailingTrivia(nil)
				}

				value = &model.TupleConsExpression{
					Tokens:      tokens,
					Expressions: objects,
				}
			} else {
				value = objects[0]

				obj := objects[0].(*model.ObjectConsExpression)
				tokens.Name.LeadingTrivia = obj.Tokens.OpenBrace.LeadingTrivia
				obj.Tokens.OpenBrace.LeadingTrivia = syntax.TriviaList{syntax.NewWhitespace(' ')}
			}

			items = append(items, &model.Attribute{
				Tokens: tokens,
				Name:   block.Type,
				Value:  value,
			})
		}
		item.Body.Items = items
	}

	return item, diagnostics
}

func (b *tf12binder) rewriteExpression(n model.Expression, resource *resource) (model.Expression, hcl.Diagnostics) {
	visitor := func(n model.Expression) (model.Expression, hcl.Diagnostics) {
		switch n := n.(type) {
		case *model.IndexExpression:
			// TODO(pdg): implement
			return n, nil
		case *model.FunctionCallExpression:
			return b.rewriteFunctionCall(n)
		case *model.RelativeTraversalExpression:
			// TODO(pdg): implement
			return n, nil
		case *model.ScopeTraversalExpression:
			return b.rewriteScopeTraversal(n, resource)
		default:
			return n, nil
		}
	}
	return model.VisitExpression(n, model.IdentityVisitor, visitor)
}

func (b *tf12binder) rewriteFunctionCall(
	n *model.FunctionCallExpression) (*model.FunctionCallExpression, hcl.Diagnostics) {

	switch n.Name {
	case "file":
		n.Name = "readFile"
	case "jsonencode":
		n.Name = "toJSON"
	}
	return n, nil
}

func internalTrivia(traversal []syntax.TraverserTokens) (syntax.TriviaList, syntax.TriviaList) {
	var leadingTrivia, trailingTrivia syntax.TriviaList
	for i, traverser := range traversal {
		switch traverser := traverser.(type) {
		case *syntax.DotTraverserTokens:
			leadingTrivia = append(leadingTrivia, traverser.Dot.AllTrivia().CollapseWhitespace()...)
			leadingTrivia = append(leadingTrivia, traverser.Index.LeadingTrivia.CollapseWhitespace()...)
			if i < len(traversal)-1 {
				leadingTrivia = append(leadingTrivia, traverser.Index.TrailingTrivia.CollapseWhitespace()...)
			} else {
				trailingTrivia = append(trailingTrivia, traverser.Index.TrailingTrivia...)
			}
		case *syntax.BracketTraverserTokens:
			leadingTrivia = append(leadingTrivia, traverser.OpenBracket.AllTrivia().CollapseWhitespace()...)
			leadingTrivia = append(leadingTrivia, traverser.Index.AllTrivia().CollapseWhitespace()...)
			leadingTrivia = append(leadingTrivia, traverser.CloseBracket.LeadingTrivia.CollapseWhitespace()...)
			if i < len(traversal)-1 {
				leadingTrivia = append(leadingTrivia, traverser.Index.TrailingTrivia.CollapseWhitespace()...)
			} else {
				trailingTrivia = append(trailingTrivia, traverser.Index.TrailingTrivia...)
			}
		}
	}
	return leadingTrivia, trailingTrivia
}

func makeSimpleTraversal(name string, part model.Traversable,
	original *model.ScopeTraversalExpression) *model.ScopeTraversalExpression {

	x := &model.ScopeTraversalExpression{
		RootName:  name,
		Traversal: hcl.Traversal{hcl.TraverseRoot{Name: name}},
		Parts:     []model.Traversable{part},
	}

	trivia, _ := internalTrivia(original.Tokens.Traversal)
	x.SetLeadingTrivia(original.GetLeadingTrivia())
	x.SetTrailingTrivia(append(trivia, original.GetTrailingTrivia()...))

	return x
}

func (b *tf12binder) rewriteScopeTraversal(n *model.ScopeTraversalExpression,
	res *resource) (*model.ScopeTraversalExpression, hcl.Diagnostics) {

	name, offset := "", 0
	var schemas il.Schemas
	for i, p := range n.Parts {
		if splat, isSplat := p.(*model.SplatVariable); isSplat {
			p = &splat.Variable
		}

		switch p := p.(type) {
		case *local:
			name, offset, schemas = p.pulumiName, i, p.schemas
		case *resource:
			name, offset, schemas = p.pulumiName, i, p.schemas
		case *module:
			name, offset = p.pulumiName, i
		case *variable:
			name, offset = p.pulumiName, i
		case *model.Variable:
			if res != nil && res.isDataSource && p == res.rangeVariable {
				if res.isCounted {
					return makeSimpleTraversal("__index", p, n), nil
				} else if len(n.Traversal) > i+1 {
					if attr, ok := n.Traversal[i+1].(hcl.TraverseAttr); ok {
						switch attr.Name {
						case "key":
							return makeSimpleTraversal("__key", p, n), nil
						case "value":
							return makeSimpleTraversal("__value", p, n), nil
						}
					}
				}
			}

			fn, ok := b.variableToSchemas[p]
			if ok {
				schemas = fn()
			}
			name, offset = p.Name, i
		case *model.Scope:
			// OK
			continue
		default:
			return n, nil
		}
		break
	}

	if n.Tokens == nil {
		n.Tokens = syntax.NewScopeTraversalTokens(n.Traversal)
	} else {
		contract.Assert(len(n.Tokens.Traversal) == len(n.Traversal)-1)
	}

	var newTraverserTokens []syntax.TraverserTokens
	traverserTokens := n.Tokens.Traversal[offset:]

	newTraversal := hcl.Traversal{hcl.TraverseRoot{
		Name:     name,
		SrcRange: n.Traversal[offset].SourceRange(),
	}}
	newParts := []model.Traversable{n.Parts[offset]}

	traversal, parts := n.Traversal[offset+1:], n.Parts[offset+1:]
	for i, traverser := range traversal {
		switch traverser := traverser.(type) {
		case hcl.TraverseAttr:
			schemas = schemas.PropertySchemas(traverser.Name)
			if schemas.Pulumi != nil && schemas.Pulumi.Name != "" {
				traverser.Name = schemas.Pulumi.Name
			} else {
				traverser.Name = tfbridge.TerraformToPulumiName(traverser.Name, schemas.TF, schemas.Pulumi, false)
			}
			newTraversal = append(newTraversal, traverser)
		case hcl.TraverseIndex:
			_, isList := model.GetTraversableType(parts[i]).(*model.ListType)
			if res, isResource := n.Parts[offset].(*resource); isResource {
				if res.isConditional {
					// Ignore indices into conditional resources.
					continue
				}
			}
			projectListElement := isList && tfbridge.IsMaxItemsOne(schemas.TF, schemas.Pulumi)

			schemas = schemas.ElemSchemas()
			if projectListElement {
				// TODO(pdg): transfer trivia to next element
				continue
			}
			newTraversal = append(newTraversal, traverser)
		default:
			contract.Failf("unexpected traverser of type %T (%v)", traverser, traverser.SourceRange())
		}
		if i < len(traverserTokens) {
			newTraverserTokens = append(newTraverserTokens, traverserTokens[i])
		}
		newParts = append(newParts, parts[i])
	}

	newTokens := syntax.NewScopeTraversalTokens(newTraversal)
	newTokens.Traversal = newTraverserTokens
	if offset == 0 {
		newTokens.Root.LeadingTrivia = n.Tokens.Root.LeadingTrivia
		newTokens.Root.TrailingTrivia = n.Tokens.Root.TrailingTrivia
	} else {
		var leadingTrivia syntax.TriviaList
		leadingTrivia = append(leadingTrivia, n.Tokens.Root.LeadingTrivia...)
		leadingTrivia = append(leadingTrivia, n.Tokens.Root.TrailingTrivia.CollapseWhitespace()...)

		trivia, trailingTrivia := internalTrivia(n.Tokens.Traversal[:offset])
		leadingTrivia = append(leadingTrivia, trivia...)

		newTokens.Root.LeadingTrivia = leadingTrivia
		newTokens.Root.TrailingTrivia = trailingTrivia
	}

	n.Tokens, n.Parts, n.Traversal, n.RootName = newTokens, newParts, newTraversal, name
	return n, nil
}

func (b *tf12binder) genResource(w io.Writer, r *resource) hcl.Diagnostics {
	var diagnostics hcl.Diagnostics

	if r.rangeVariable != nil {
		r.rangeVariable.Name = "range"
	}

	rewriter := &resourceRewriter{
		binder:   b,
		resource: r,
	}
	_, diags := model.VisitBodyItem(r.block, rewriter.enterBodyItem, rewriter.rewriteBodyItem)
	diagnostics = append(diagnostics, diags...)

	item := model.BodyItem(r.block)
	if !r.isDataSource {
		r.block.Labels = []string{r.pulumiName, r.token}

		if r.isConditional {
			options := r.block.Body.Blocks("options")[0]
			rng, _ := options.Body.Attribute("range")

			litTokens := syntax.NewLiteralValueTokens(cty.True)
			litTokens.Value[0].TrailingTrivia = rng.Value.GetTrailingTrivia()
			rng.Value.SetTrailingTrivia(nil)

			rng.Value = &model.BinaryOpExpression{
				LeftOperand: rng.Value,
				Operation:   hclsyntax.OpEqual,
				RightOperand: &model.LiteralValueExpression{
					Tokens: litTokens,
					Value:  cty.True,
				},
			}
		}
	} else {
		var options *model.Block
		bodyItems := make([]model.BodyItem, 0, len(r.block.Body.Items))
		for _, item := range r.block.Body.Items {
			if block, ok := item.(*model.Block); ok {
				contract.Assert(block.Type == "options")
				options = block
				continue
			}
			bodyItems = append(bodyItems, item)
		}
		r.block.Body.Items = bodyItems

		obj := rewriter.rewriteBlockAsObjectCons(r.block)

		valueTokens := syntax.NewFunctionCallTokens("invoke", 2)
		valueTokens.Name.LeadingTrivia = syntax.TriviaList{syntax.NewWhitespace(' ')}
		valueTokens.CloseParen.TrailingTrivia = obj.Tokens.CloseBrace.TrailingTrivia
		obj.Tokens.CloseBrace.TrailingTrivia = nil
		value := model.Expression(&model.FunctionCallExpression{
			Tokens: valueTokens,
			Name:   "invoke",
			Args: []model.Expression{
				&model.TemplateExpression{
					Parts: []model.Expression{&model.LiteralValueExpression{
						Value: cty.StringVal(r.token),
					}},
				},
				obj,
			},
		})

		var rangeExpr model.Expression
		if options != nil {
			if rng, hasRange := options.Body.Attribute("range"); hasRange {
				rangeExpr = rng.Value
				if r.isCounted {
					rangeExpr = &model.FunctionCallExpression{
						Name: "range",
						Args: []model.Expression{rng.Value},
					}
				}
			}
		}
		if rangeExpr != nil {
			if r.isConditional {
				traversal := hcl.Traversal{hcl.TraverseRoot{Name: "null"}}
				nullTokens := syntax.NewScopeTraversalTokens(traversal)
				nullTokens.Root.TrailingTrivia = valueTokens.CloseParen.TrailingTrivia
				valueTokens.CloseParen.TrailingTrivia = nil

				value = &model.ConditionalExpression{
					Tokens:     syntax.NewConditionalTokens(),
					Condition:  rangeExpr,
					TrueResult: value,
					FalseResult: &model.ScopeTraversalExpression{
						Tokens:    nullTokens,
						Traversal: traversal,
						Parts: []model.Traversable{
							&model.Constant{
								Name:          "null",
								ConstantValue: cty.NullVal(cty.DynamicPseudoType),
							},
						},
					},
				}
			} else {
				valueVariable := &model.Variable{
					Name:         "__index",
					VariableType: model.DynamicType,
				}
				keyVariableName := ""
				var keyVariable *model.Variable
				if !r.isCounted {
					valueVariable.Name = "__value"
					keyVariableName = "__key"
					keyVariable = &model.Variable{
						Name:         "__key",
						VariableType: model.DynamicType,
					}
				}

				forTokens := syntax.NewForTokens(keyVariableName, valueVariable.Name, false, false, false)
				forTokens.Close.TrailingTrivia = valueTokens.CloseParen.TrailingTrivia
				valueTokens.CloseParen.TrailingTrivia = nil

				value = &model.ForExpression{
					Tokens:        forTokens,
					KeyVariable:   keyVariable,
					ValueVariable: valueVariable,
					Collection:    rangeExpr,
					Value:         value,
				}
			}
		}

		attrTokens := syntax.NewAttributeTokens(r.pulumiName)
		attrTokens.Name.LeadingTrivia = obj.Tokens.OpenBrace.LeadingTrivia
		obj.Tokens.OpenBrace.LeadingTrivia = syntax.TriviaList{syntax.NewWhitespace(' ')}
		item = &model.Attribute{
			Tokens: attrTokens,
			Name:   r.pulumiName,
			Value:  value,
		}
	}
	fmt.Fprintf(w, "%v", item)
	return diagnostics
}

func (b *tf12binder) resourceType(addr addrs.Resource,
	subject hcl.Range) (string, il.Schemas, model.Type, hcl.Diagnostics) {

	providerName := addr.ImpliedProvider()

	info, ok := b.providers[providerName]
	if !ok {
		i, err := b.providerInfo.GetProviderInfo(providerName)
		if err != nil {
			// Fake up a root-level token.
			tok := providerName + ":index:" + addr.Type
			return tok, il.Schemas{}, model.DynamicType, hcl.Diagnostics{{
				Subject: &subject,
				Summary: fmt.Sprintf("unknown provider '%s'", providerName),
				Detail:  fmt.Sprintf("unknown provider '%s'", providerName),
			}}
		}
		info, b.providers[providerName] = i, i
	}

	token := addr.Type
	var schemas il.Schemas
	if addr.Mode == addrs.ManagedResourceMode {
		schemaInfo := &tfbridge.SchemaInfo{}
		if resInfo, ok := info.Resources[addr.Type]; ok {
			token = string(resInfo.Tok)
			schemaInfo.Fields = resInfo.Fields
		}
		schemas.TFRes = info.P.ResourcesMap[addr.Type]
		schemas.Pulumi = schemaInfo
	} else {
		schemaInfo := &tfbridge.SchemaInfo{}
		if dsInfo, ok := info.DataSources[addr.Type]; ok {
			token = string(dsInfo.Tok)
			schemaInfo.Fields = dsInfo.Fields
		}
		schemas.TFRes = info.P.DataSourcesMap[addr.Type]
		schemas.Pulumi = schemaInfo
	}
	if schemas.TFRes == nil {
		schemas.TFRes = &schema.Resource{Schema: map[string]*schema.Schema{}}
	}
	schemas.TFRes.Schema["id"] = &schema.Schema{Type: schema.TypeString, Computed: true}

	return token, schemas, schemas.ModelType(), nil
}

func (b *tf12binder) providerType(providerName string,
	subject hcl.Range) (string, il.Schemas, model.Type, hcl.Diagnostics) {

	tok := "pulumi:providers:" + providerName

	info, ok := b.providers[providerName]
	if !ok {
		i, err := b.providerInfo.GetProviderInfo(providerName)
		if err != nil {
			return tok, il.Schemas{}, model.DynamicType, hcl.Diagnostics{{
				Subject: &subject,
				Summary: fmt.Sprintf("unknown provider '%s'", providerName),
				Detail:  fmt.Sprintf("unknown provider '%s'", providerName),
			}}
		}
		info, b.providers[providerName] = i, i
	}

	schemas := il.Schemas{
		Pulumi: &tfbridge.SchemaInfo{
			Fields: info.Config,
		},
		TFRes: &schema.Resource{
			Schema: info.P.Schema,
		},
	}
	return tok, schemas, schemas.ModelType(), nil
}

var tf12builtins = map[string]*model.Function{
	"cidrsubnet": model.NewFunction(model.StaticFunctionSignature{
		Parameters: []model.Parameter{
			{
				Name: "prefix",
				Type: model.StringType,
			},
			{
				Name: "newbits",
				Type: model.NumberType,
			},
			{
				Name: "netnum",
				Type: model.NumberType,
			},
		},
		ReturnType: model.StringType,
	}),
	"element": model.NewFunction(model.GenericFunctionSignature(
		func(args []model.Expression) (model.StaticFunctionSignature, hcl.Diagnostics) {
			var diagnostics hcl.Diagnostics

			listType, returnType := model.Type(model.DynamicType), model.Type(model.DynamicType)
			if len(args) > 0 {
				switch t := args[0].Type().(type) {
				case *model.ListType:
					listType, returnType = t, t.ElementType
				case *model.TupleType:
					_, elementType := model.UnifyTypes(t.ElementTypes...)
					listType, returnType = t, elementType
				default:
					rng := args[0].SyntaxNode().Range()
					diagnostics = hcl.Diagnostics{&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "the first argument to 'element' must be a list or tuple",
						Subject:  &rng,
					}}
				}
			}
			return model.StaticFunctionSignature{
				Parameters: []model.Parameter{
					{
						Name: "list",
						Type: listType,
					},
					{
						Name: "index",
						Type: model.NumberType,
					},
				},
				ReturnType: returnType,
			}, diagnostics
		})),
	"file": model.NewFunction(model.StaticFunctionSignature{
		Parameters: []model.Parameter{{
			Name: "path",
			Type: model.StringType,
		}},
		ReturnType: model.StringType,
	}),
	"jsonencode": model.NewFunction(model.StaticFunctionSignature{
		Parameters: []model.Parameter{{
			Name: "value",
			Type: model.DynamicType,
		}},
		ReturnType: model.StringType,
	}),
	"length": model.NewFunction(model.GenericFunctionSignature(
		func(args []model.Expression) (model.StaticFunctionSignature, hcl.Diagnostics) {
			var diagnostics hcl.Diagnostics

			valueType := model.Type(model.DynamicType)
			if len(args) > 0 {
				valueType = args[0].Type()
				switch valueType := valueType.(type) {
				case *model.ListType, *model.MapType, *model.ObjectType, *model.TupleType:
					// OK
				default:
					if model.StringType.ConversionFrom(valueType) == model.NoConversion {
						rng := args[0].SyntaxNode().Range()
						diagnostics = hcl.Diagnostics{&hcl.Diagnostic{
							Severity: hcl.DiagError,
							Summary:  "the first argument to 'element' must be a list, map, object, tuple, or string",
							Subject:  &rng,
						}}
					}
				}
			}
			return model.StaticFunctionSignature{
				Parameters: []model.Parameter{{
					Name: "value",
					Type: valueType,
				}},
				ReturnType: model.IntType,
			}, diagnostics
		})),
	"lookup": model.NewFunction(model.GenericFunctionSignature(
		func(args []model.Expression) (model.StaticFunctionSignature, hcl.Diagnostics) {
			var diagnostics hcl.Diagnostics

			mapType, elementType := model.Type(model.DynamicType), model.Type(model.DynamicType)
			if len(args) > 0 {
				switch t := args[0].Type().(type) {
				case *model.MapType:
					mapType, elementType = t, t.ElementType
				case *model.ObjectType:
					var unifiedType model.Type
					for _, t := range t.Properties {
						_, unifiedType = model.UnifyTypes(unifiedType, t)
					}
					mapType, elementType = t, unifiedType
				default:
					rng := args[0].SyntaxNode().Range()
					diagnostics = hcl.Diagnostics{&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "the first argument to 'lookup' must be a map or object",
						Subject:  &rng,
					}}
				}
			}
			return model.StaticFunctionSignature{
				Parameters: []model.Parameter{
					{
						Name: "map",
						Type: mapType,
					},
					{
						Name: "key",
						Type: model.StringType,
					},
					{
						Name: "default",
						Type: model.NewOptionalType(elementType),
					},
				},
				ReturnType: elementType,
			}, diagnostics
		})),
	"split": model.NewFunction(model.StaticFunctionSignature{
		Parameters: []model.Parameter{
			{
				Name: "separator",
				Type: model.StringType,
			},
			{
				Name: "string",
				Type: model.StringType,
			},
		},
		ReturnType: model.NewListType(model.StringType),
	}),
}

package convert

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/hcl/token"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hil/ast"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/gen/nodejs"
	"github.com/pulumi/tf2pulumi/gen/python"
	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
	tf11module "github.com/pulumi/tf2pulumi/internal/config/module"
)

// convertTF11 converts a TF11 graph to a set of TF12 files.
func convertTF11(opts Options) (map[string][]byte, bool, error) {
	moduleStorage := tf11module.NewStorage(filepath.Join(".terraform", "modules"))

	mod, err := tf11module.NewTreeFs("", opts.Root)
	if err != nil {
		return nil, true, fmt.Errorf("failed to create tree: %w", err)
	}

	if err = mod.Load(moduleStorage); err != nil {
		return nil, true, fmt.Errorf("failed to load module: %w", err)
	}

	gs, err := buildGraphs(mod, opts)
	if err != nil {
		return nil, true, fmt.Errorf("failed to build graphs: %w", err)
	}

	if opts.TerraformVersion == "12" || opts.TargetLanguage != "typescript" {
		// Generate TF12 code from the TF11 graph, then pass the result off to the TF12 pipeline.
		g := &tf11generator{}
		g.Emitter = gen.NewEmitter(nil, g)
		files, err := g.genModules(gs)
		return files, true, err
	}

	// Filter resource name properties if requested.
	if opts.FilterResourceNames {
		filterAutoNames := opts.ResourceNameProperty == ""
		for _, g := range gs {
			for _, r := range g.Resources {
				if r.Config.Mode == config.ManagedResourceMode {
					il.FilterProperties(r, func(key string, _ il.BoundNode) bool {
						if filterAutoNames {
							sch := r.Schemas().PropertySchemas(key).Pulumi
							return sch == nil || sch.Default == nil || !sch.Default.AutoNamed
						}
						return key != opts.ResourceNameProperty
					})
				}
			}
		}
	}

	// Annotate nodes with the location of their original definition if requested.
	if opts.AnnotateNodesWithLocations {
		for _, g := range gs {
			addLocationAnnotations(g)
		}
	}

	var buf bytes.Buffer

	generator, filename, err := newGenerator(&buf, "auto", opts)
	if err != nil {
		return nil, false, errors.Wrapf(err, "creating generator")
	}

	if err = gen.Generate(gs, generator); err != nil {
		return nil, false, err
	}

	files := map[string][]byte{
		filename: buf.Bytes(),
	}
	return files, false, nil
}

func addLocationAnnotation(location token.Pos, comments **il.Comments) {
	if !location.IsValid() {
		return
	}

	c := *comments
	if c == nil {
		c = &il.Comments{}
		*comments = c
	}

	if len(c.Leading) != 0 {
		c.Leading = append(c.Leading, "")
	}
	c.Leading = append(c.Leading, fmt.Sprintf(" Originally defined at %v:%v", location.Filename, location.Line))
}

// addLocationAnnotations adds comments that record the original source location of each top-level node in a module.
func addLocationAnnotations(m *il.Graph) {
	for _, n := range m.Modules {
		addLocationAnnotation(n.Location, &n.Comments)
	}
	for _, n := range m.Providers {
		addLocationAnnotation(n.Location, &n.Comments)
	}
	for _, n := range m.Resources {
		addLocationAnnotation(n.Location, &n.Comments)
	}
	for _, n := range m.Outputs {
		addLocationAnnotation(n.Location, &n.Comments)
	}
	for _, n := range m.Locals {
		addLocationAnnotation(n.Location, &n.Comments)
	}
	for _, n := range m.Variables {
		addLocationAnnotation(n.Location, &n.Comments)
	}
}

func buildGraphs(tree *tf11module.Tree, opts Options) ([]*il.Graph, error) {
	// TODO: move this into the il package and unify modules based on path

	children := []*il.Graph{}
	for _, c := range tree.Children() {
		cc, err := buildGraphs(c, opts)
		if err != nil {
			return nil, err
		}
		children = append(children, cc...)
	}

	buildOpts := il.BuildOptions{
		AllowMissingProviders: opts.AllowMissingProviders,
		AllowMissingVariables: opts.AllowMissingVariables,
		AllowMissingComments:  opts.AllowMissingComments,
		ProviderInfoSource:    opts.ProviderInfoSource,
		Logger:                opts.Logger,
	}
	g, err := il.BuildGraph(tree, &buildOpts)
	if err != nil {
		return nil, err
	}

	return append(children, g), nil
}

func newGenerator(w io.Writer, projectName string, opts Options) (gen.Generator, string, error) {
	switch opts.TargetLanguage {
	case LanguageTypescript:
		nodeOpts, ok := opts.TargetOptions.(nodejs.Options)
		if !ok && opts.TargetOptions != nil {
			return nil, "", errors.Errorf("invalid target options of type %T", opts.TargetOptions)
		}
		g, err := nodejs.New(projectName, opts.TargetSDKVersion, nodeOpts.UsePromptDataSources, w)
		if err != nil {
			return nil, "", err
		}
		return g, "index.ts", nil
	case LanguagePython:
		return python.New(projectName, w), "__main__.py", nil
	default:
		return nil, "", errors.Errorf("invalid language '%s', expected one of %s",
			opts.TargetLanguage, strings.Join(ValidLanguages[:], ", "))
	}
}

// tf11generator generates Typescript code that targets the Pulumi libraries from a Terraform configuration.
type tf11generator struct {
	// The emitter to use when generating code.
	*gen.Emitter

	exprStack []il.BoundExpr
}

func (g *tf11generator) genModules(modules []*il.Graph) (map[string][]byte, error) {
	sources := map[string][]il.Node{}
	addNode := func(n il.Node) {
		filename := n.GetLocation().Filename
		if filename == "" {
			filename = "main.tf"
		}
		sources[filename] = append(sources[filename], n)
	}

	for _, mod := range modules {
		for _, n := range mod.Variables {
			addNode(n)
		}
		for _, n := range mod.Modules {
			addNode(n)
		}
		for _, n := range mod.Providers {
			addNode(n)
		}
		for _, n := range mod.Resources {
			addNode(n)
		}
		for _, n := range mod.Locals {
			addNode(n)
		}
		for _, n := range mod.Outputs {
			addNode(n)
		}
	}

	outputs := map[string][]byte{}
	for _, filename := range gen.SortedKeys(sources) {
		nodes := sources[filename]

		sort.Slice(nodes, func(i, j int) bool {
			al, bl := nodes[i].GetLocation(), nodes[j].GetLocation()
			if al.Line < bl.Line {
				return true
			}
			if al.Line > bl.Line {
				return false
			}
			if al.Column < bl.Column {
				return true
			}
			if al.Column > bl.Column {
				return false
			}
			return nodes[i].ID() < nodes[j].ID()
		})

		var buf bytes.Buffer
		var locals []*il.LocalNode
		for _, n := range nodes {
			if local, isLocal := n.(*il.LocalNode); isLocal {
				locals = append(locals, local)
				continue
			}

			if err := g.genLocals(&buf, locals); err != nil {
				return nil, err
			}
			locals = nil

			switch n := n.(type) {
			case *il.VariableNode:
				if err := g.genVariable(&buf, n); err != nil {
					return nil, err
				}
			case *il.ModuleNode:
				if err := g.genModule(&buf, n); err != nil {
					return nil, err
				}
			case *il.ProviderNode:
				if err := g.genProvider(&buf, n); err != nil {
					return nil, err
				}
			case *il.ResourceNode:
				if err := g.genResource(&buf, n); err != nil {
					return nil, err
				}
			case *il.OutputNode:
				if err := g.genOutput(&buf, n); err != nil {
					return nil, err
				}
			}
		}
		if err := g.genLocals(&buf, locals); err != nil {
			return nil, err
		}
		locals = nil

		outputs[filename] = buf.Bytes()
	}

	return outputs, nil
}

// genLeadingComment generates a leading comment into the output.
func (g *tf11generator) genLeadingComment(w io.Writer, comments *il.Comments) {
	if comments == nil {
		return
	}
	for _, l := range comments.Leading {
		g.Fgenf(w, "%s//%s\n", g.Indent, l)
	}
}

// genTrailing comment generates a trailing comment into the output.
func (g *tf11generator) genTrailingComment(w io.Writer, comments *il.Comments) {
	if comments == nil {
		return
	}

	// If this is a single-line comment, generate it as-is. Otherwise, add a line break and generate it as a block.
	if len(comments.Trailing) == 1 {
		g.Fgenf(w, " //%s", comments.Trailing[0])
	} else {
		for _, l := range comments.Trailing {
			g.Fgenf(w, "\n%s//%s", g.Indent, l)
		}
	}
}

// genVariable generates definitions for the set of user variables in the context of the current module.
func (g *tf11generator) genVariable(w io.Writer, v *il.VariableNode) error {
	g.genLeadingComment(w, v.Comments)
	g.Fgenf(w, "%svariable \"%s\" {", g.Indent, v.Config.Name)
	if v.DefaultValue != nil {
		g.Indented(func() {
			g.Fgenf(w, "\n%sdefault = %v", g.Indent, v.DefaultValue)
		})
		g.Fgenf(w, "\n%s", g.Indent)
	}
	g.Fgenf(w, "}")
	g.genTrailingComment(w, v.Comments)
	g.Fgenf(w, "\n")

	return nil
}

// genLocals generates a series of local variables.
func (g *tf11generator) genLocals(w io.Writer, locals []*il.LocalNode) error {
	// If there are no locals, we have nothing to do.
	if len(locals) == 0 {
		return nil
	}

	g.Fgenf(w, "%slocals {", g.Indent)
	g.Indented(func() {
		for _, l := range locals {
			g.genLeadingComment(w, l.Comments)
			g.Fgenf(w, "\n%s%s = %v", g.Indent, l.Config.Name, l.Value)
			g.genTrailingComment(w, l.Comments)
		}
	})
	g.Fgenf(w, "%s\n}\n", g.Indent)

	return nil
}

// genModule generates a single module instantiation. A module instantiation is generated as a call to the
// appropriate module factory function; the result is assigned to a local variable.
func (g *tf11generator) genModule(w io.Writer, m *il.ModuleNode) error {
	g.genLeadingComment(w, m.Comments)
	g.Fgenf(w, "%smodule \"%s\" %v", g.Indent, m.Config.Name, m.Properties)
	g.genTrailingComment(w, m.Comments)
	g.Fgenf(w, "\n")

	return nil
}

// genProvider generates a single provider instantiation.
func (g *tf11generator) genProvider(w io.Writer, p *il.ProviderNode) error {
	if p.Implicit {
		return nil
	}

	if p.Config.Alias != "" {
		p.Properties.Elements["alias"] = &il.BoundLiteral{
			ExprType: il.TypeString,
			Value:    p.Config.Alias,
		}
	}

	g.genLeadingComment(w, p.Comments)
	g.Fgenf(w, "%sprovider \"%s\" %v", g.Indent, p.Config.Name, p.Properties)
	g.genTrailingComment(w, p.Comments)
	g.Fgenf(w, "\n")

	return nil
}

// genResource generates a single resource instantiation. Each resource instantiation is generated as a call or
// sequence of calls (in the case of a counted resource) to the approriate resource constructor or data source
// function. Single-instance resources are assigned to a local variable; counted resources are stored in an array-typed
// local.
func (g *tf11generator) genResource(w io.Writer, r *il.ResourceNode) error {
	// Build the lifeycle block if necessary.
	if len(r.IgnoreChanges) != 0 || r.Config.Lifecycle.PreventDestroy {
		lifecycle := &il.BoundMapProperty{Elements: map[string]il.BoundNode{}}
		if len(r.IgnoreChanges) != 0 {
			ignoreChanges := &il.BoundListProperty{}
			for _, prop := range r.IgnoreChanges {
				ignoreChanges.Elements = append(ignoreChanges.Elements, &il.BoundLiteral{
					ExprType: il.TypeString,
					Value:    prop,
				})
			}
			lifecycle.Elements["ignore_changes"] = ignoreChanges
		}
		if r.Config.Lifecycle.PreventDestroy {
			lifecycle.Elements["prevent_destroy"] = &il.BoundLiteral{ExprType: il.TypeBool, Value: true}
		}
		r.Properties.Elements["lifecycle"] = lifecycle
	}

	// Set other meta properties.
	if len(r.Config.DependsOn) != 0 {
		dependsOn := &il.BoundListProperty{}
		for _, dep := range r.Config.DependsOn {
			dependsOn.Elements = append(dependsOn.Elements, &il.BoundLiteral{
				ExprType: il.TypeString,
				Value:    dep,
			})
		}
		r.Properties.Elements["depends_on"] = dependsOn
	}
	if r.Count != nil {
		r.Properties.Elements["count"] = r.Count
	}
	if r.Provider.Config.Alias != "" {
		r.Properties.Elements["provider"] = &il.BoundLiteral{
			ExprType: il.TypeString,
			Value:    r.Provider.Config.FullName(),
		}
	}

	typ := "resource"
	if r.IsDataSource {
		typ = "data"
	}

	g.genLeadingComment(w, r.Comments)
	g.Fgenf(w, "%s%s \"%s\" \"%s\" %v", g.Indent, typ, r.Config.Type, r.Config.Name, r.Properties)
	g.genTrailingComment(w, r.Comments)
	g.Fgenf(w, "\n")
	return nil
}

// genOutput generates a Terraform output.
func (g *tf11generator) genOutput(w io.Writer, o *il.OutputNode) error {
	g.genLeadingComment(w, o.Comments)
	g.Fgenf(w, "%soutput \"%s\" {\n", g.Indent, o.Config.Name)
	g.Indented(func() {
		g.Fgenf(w, "%svalue = %v\n", g.Indent, o.Value)
	})
	g.Fgenf(w, "%s}", g.Indent)
	g.genTrailingComment(w, o.Comments)
	g.Fgenf(w, "\n")

	return nil
}

func (g *tf11generator) pushExpr(n il.BoundExpr) {
	g.exprStack = append(g.exprStack, n)
}

func (g *tf11generator) popExpr() {
	g.exprStack = g.exprStack[:len(g.exprStack)-1]
}

func (g *tf11generator) inExpr() bool {
	return len(g.exprStack) > 0
}

// GenArithmetic generates code for the given arithmetic expression.
func (g *tf11generator) GenArithmetic(w io.Writer, n *il.BoundArithmetic) {
	g.pushExpr(n)
	defer g.popExpr()

	op := ""
	switch n.Op {
	case ast.ArithmeticOpAdd:
		op = "+"
	case ast.ArithmeticOpSub:
		op = "-"
	case ast.ArithmeticOpMul:
		op = "*"
	case ast.ArithmeticOpDiv:
		op = "/"
	case ast.ArithmeticOpMod:
		op = "%"
	case ast.ArithmeticOpLogicalAnd:
		op = "&&"
	case ast.ArithmeticOpLogicalOr:
		op = "||"
	case ast.ArithmeticOpEqual:
		op = "=="
	case ast.ArithmeticOpNotEqual:
		op = "!="
	case ast.ArithmeticOpLessThan:
		op = "<"
	case ast.ArithmeticOpLessThanOrEqual:
		op = "<="
	case ast.ArithmeticOpGreaterThan:
		op = ">"
	case ast.ArithmeticOpGreaterThanOrEqual:
		op = ">="
	}
	op = fmt.Sprintf(" %s ", op)

	g.Fgen(w, "(")
	for i, n := range n.Exprs {
		if i != 0 {
			g.Fgen(w, op)
		}
		g.Fgen(w, n)
	}
	g.Fgen(w, ")")
}

// GenCall generates code for a call expression.
func (g *tf11generator) GenCall(w io.Writer, n *il.BoundCall) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgenf(w, "%v(", n.Func)
	for i, arg := range n.Args {
		if i > 0 {
			g.Fgenf(w, ", ")
		}
		g.Fgenf(w, "%v", arg)
	}
	g.Fgenf(w, ")")
}

// GenConditional generates code for a single conditional expression.
func (g *tf11generator) GenConditional(w io.Writer, n *il.BoundConditional) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgenf(w, "(%v ? %v : %v)", n.CondExpr, n.TrueExpr, n.FalseExpr)
}

// GenError generates code for a single error expression.
func (g *tf11generator) GenError(w io.Writer, n *il.BoundError) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgenf(w, "error(%q)", n.Error.Error())
}

// GenIndex generates code for a single index expression.
func (g *tf11generator) GenIndex(w io.Writer, n *il.BoundIndex) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgenf(w, "%v[%v]", n.TargetExpr, n.KeyExpr)
}

func (g *tf11generator) genEscapedString(b *strings.Builder, v string, heredoc bool) {
	for _, c := range v {
		switch c {
		case '"', '\\':
			if !heredoc {
				b.WriteRune('\\')
			}
		case '$':
			b.WriteRune('$')
		}
		b.WriteRune(c)
	}
}

func (g *tf11generator) genStringLiteral(w io.Writer, v string) {
	builder := strings.Builder{}

	heredoc, start, end := false, `"`, `"`
	if strings.IndexRune(v, '\n') != -1 {
		heredoc, start, end = true, "<<EOF\n", "\nEOF"
	}

	builder.WriteString(start)
	g.genEscapedString(&builder, v, heredoc)
	builder.WriteString(end)

	g.Fgen(w, builder.String())
}

// genListProperty generates code for as single list property.
func (g *tf11generator) GenListProperty(w io.Writer, n *il.BoundListProperty) {
	switch len(n.Elements) {
	case 0:
		g.Fgen(w, "[]")
	case 1:
		// We can ignore comments in this case: the comment extractor will never associate comments with a
		// single-element list.
		v := n.Elements[0]
		if v.Type().IsList() {
			// TF11 and below flatten list elements that are themselves lists into the parent list.
			g.Fgenf(w, "%v", v)
		} else {
			g.Fgenf(w, "[%v]", v)
		}
	default:
		g.Fgen(w, "[")
		g.Indented(func() {
			for _, v := range n.Elements {
				g.Fgenf(w, "\n")
				g.genLeadingComment(w, v.Comments())
				g.Fgenf(w, "%s%v,", g.Indent, v)
				g.genTrailingComment(w, v.Comments())
			}
		})
		g.Fgen(w, "\n", g.Indent, "]")
	}
}

// GenLiteral generates code for a single literal expression
func (g *tf11generator) GenLiteral(w io.Writer, n *il.BoundLiteral) {
	g.pushExpr(n)
	defer g.popExpr()

	switch n.ExprType {
	case il.TypeBool:
		g.Fgenf(w, "%v", n.Value)
	case il.TypeNumber:
		f := n.Value.(float64)
		if float64(int64(f)) == f {
			g.Fgenf(w, "%d", int64(f))
		} else {
			g.Fgenf(w, "%g", n.Value)
		}
	case il.TypeString:
		g.genStringLiteral(w, n.Value.(string))
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", n.ExprType)
	}
}

// genMapProperty generates code for a single map property.
func (g *tf11generator) GenMapProperty(w io.Writer, n *il.BoundMapProperty) {
	if len(n.Elements) == 0 {
		g.Fgen(w, "{}")
	} else {
		g.Fgen(w, "{")
		g.Indented(func() {
			for _, k := range gen.SortedKeys(n.Elements) {
				v := n.Elements[k]

				key := k
				if !hclsyntax.ValidIdentifier(key) {
					key = fmt.Sprintf("%q", key)
				}

				g.Fgenf(w, "\n")
				g.genLeadingComment(w, v.Comments())
				g.Fgenf(w, "%s%s = %v", g.Indent, key, v)
				if g.inExpr() {
					g.Fgenf(w, ",")
				}
				g.genTrailingComment(w, v.Comments())
			}
		})
		g.Fgen(w, "\n", g.Indent, "}")
	}
}

// GenOutput generates code for a single output expression.
func (g *tf11generator) GenOutput(w io.Writer, n *il.BoundOutput) {
	g.pushExpr(n)
	defer g.popExpr()

	heredoc, start, end := false, `"`, `"`
	for _, s := range n.Exprs {
		if lit, ok := s.(*il.BoundLiteral); ok && lit.ExprType == il.TypeString {
			if strings.IndexRune(lit.Value.(string), '\n') != -1 {
				heredoc, start, end = true, "<<EOF\n", "\nEOF"
				break
			}
		}
	}

	builder := strings.Builder{}
	g.Fgen(&builder, start)
	for _, s := range n.Exprs {
		if lit, ok := s.(*il.BoundLiteral); ok && lit.ExprType == il.TypeString {
			g.genEscapedString(&builder, lit.Value.(string), heredoc)
		} else {
			g.Fgenf(&builder, "${%v}", s)
		}
	}
	g.Fgen(&builder, end)

	g.Fgen(w, builder.String())
}

// GenPropertyValue generates code for a single property value expression.
func (g *tf11generator) GenPropertyValue(w io.Writer, n *il.BoundPropertyValue) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgen(w, n.Value)
}

// GenVariableAccess generates code for a single variable access expression.
func (g *tf11generator) GenVariableAccess(w io.Writer, n *il.BoundVariableAccess) {
	g.pushExpr(n)
	defer g.popExpr()

	g.Fgen(w, n.TFVar.FullKey())
}

package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/il"
)

// TODO:
// - proper use of apply
// - assets

type nodeGenerator struct {
	projectName string
	graph       *il.Graph
}

type schemas struct {
	tf     *schema.Schema
	tfRes  *schema.Resource
	pulumi *tfbridge.SchemaInfo
}

func (s schemas) propertySchemas(key string) schemas {
	var propSch schemas

	if s.tfRes != nil && s.tfRes.Schema != nil {
		propSch.tf = s.tfRes.Schema[key]
	}

	if propSch.tf != nil {
		if propResource, ok := propSch.tf.Elem.(*schema.Resource); ok {
			propSch.tfRes = propResource
		}
	}

	if s.pulumi != nil && s.pulumi.Fields != nil {
		propSch.pulumi = s.pulumi.Fields[key]
	}

	return propSch
}

func (s schemas) elemSchemas() schemas {
	var elemSch schemas

	if s.tf != nil {
		switch e := s.tf.Elem.(type) {
		case *schema.Schema:
			elemSch.tf = e
		case *schema.Resource:
			elemSch.tfRes = e
		}
	}

	if s.pulumi != nil {
		elemSch.pulumi = s.pulumi.Elem
	}

	return elemSch
}

func (s schemas) astType() ast.Type {
	if s.tf != nil {
		switch s.tf.Type {
		case schema.TypeBool:
			return ast.TypeBool
		case schema.TypeInt, schema.TypeFloat:
			return ast.TypeFloat
		case schema.TypeString:
			return ast.TypeString
		case schema.TypeList, schema.TypeSet:
			// TODO: might need to do max-items-one projection here
			return ast.TypeList
		case schema.TypeMap:
			return ast.TypeMap
		default:
			return ast.TypeUnknown
		}
	} else if s.tfRes != nil {
		return ast.TypeMap
	}

	return ast.TypeUnknown
}

func resName(typ, name string) string {
	n := fmt.Sprintf("%s_%s", typ, name)
	if strings.ContainsAny(n, " -.") {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' || r == '.' {
				return '_'
			}
			return r
		}, n)
	}
	return n
}

func tsName(tfName string, tfSchema *schema.Schema, schemaInfo *tfbridge.SchemaInfo, isObjectKey bool) string {
	if schemaInfo != nil && schemaInfo.Name != "" {
		return schemaInfo.Name
	}

	if strings.ContainsAny(tfName, " -.") {
		if isObjectKey {
			return fmt.Sprintf("\"%s\"", tfName)
		}
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' || r == '.' {
				return '_'
			}
			return r
		}, tfName)
	}
	return tfbridge.TerraformToPulumiName(tfName, tfSchema, false)
}

func coerceProperty(value string, valueType, propertyType ast.Type) string {
	// We only coerce values we know are strings.
	if valueType == propertyType || valueType != ast.TypeString {
		return value
	}

	switch propertyType {
	case ast.TypeBool:
		if value == "\"true\"" {
			return "true"
		} else if value == "\"false\"" {
			return "false"
		}
		return fmt.Sprintf("(%s === \"true\")", value)
	case ast.TypeInt, ast.TypeFloat:
		return fmt.Sprintf("Number.parseFloat(%s)", value)
	default:
		return value
	}
}

type nodeHILWalker struct {
	pc *nodePropertyComputer
}

func (w *nodeHILWalker) walkArithmetic(n *ast.Arithmetic) (string, ast.Type, error) {
	strs, _, err := w.walkNodes(n.Exprs)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

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
		op = "==="
	case ast.ArithmeticOpNotEqual:
		op = "!=="
	case ast.ArithmeticOpLessThan:
		op = "<"
	case ast.ArithmeticOpLessThanOrEqual:
		op = "<="
	case ast.ArithmeticOpGreaterThan:
		op = ">"
	case ast.ArithmeticOpGreaterThanOrEqual:
		op = ">="
	}

	return "(" + strings.Join(strs, " "+op+" ") + ")", ast.TypeFloat, nil
}

func (w *nodeHILWalker) walkCall(n *ast.Call) (string, ast.Type, error) {
	strs, _, err := w.walkNodes(n.Args)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

	switch n.Func {
	case "element":
		// TODO: wrapping semantics
		return fmt.Sprintf("%s[%s]", strs[0], strs[1]), ast.TypeUnknown, nil
	case "file":
		return fmt.Sprintf("fs.readFileSync(%s, \"utf-8\")", strs[0]), ast.TypeString, nil
	case "lookup":
		lookup := fmt.Sprintf("(<any>%s)[%s]", strs[0], strs[1])
		if len(strs) == 3 {
			lookup += fmt.Sprintf(" || %s", strs[2])
		}
		return lookup, ast.TypeUnknown, nil
	case "split":
		return fmt.Sprintf("%s.split(%s)", strs[1], strs[0]), ast.TypeList, nil
	default:
		return "", ast.TypeInvalid, errors.Errorf("NYI: call to %s", n.Func)
	}
}

func (w *nodeHILWalker) walkConditional(n *ast.Conditional) (string, ast.Type, error) {
	cond, _, err := w.walkNode(n.CondExpr)
	if err != nil {
		return "", ast.TypeInvalid, err
	}
	t, tt, err := w.walkNode(n.TrueExpr)
	if err != nil {
		return "", ast.TypeInvalid, err
	}
	f, tf, err := w.walkNode(n.FalseExpr)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

	typ := tt
	if tt == ast.TypeUnknown {
		typ = tf
	}

	return fmt.Sprintf("(%s ? %s : %s)", cond, t, f), typ, nil
}

func (w *nodeHILWalker) walkIndex(n *ast.Index) (string, ast.Type, error) {
	target, _, err := w.walkNode(n.Target)
	if err != nil {
		return "", ast.TypeInvalid, err
	}
	key, _, err := w.walkNode(n.Key)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

	return fmt.Sprintf("%s[%s]", target, key), ast.TypeUnknown, nil
}

func (w *nodeHILWalker) walkLiteral(n *ast.LiteralNode) (string, ast.Type, error) {
	switch n.Typex {
	case ast.TypeBool, ast.TypeInt, ast.TypeFloat:
		return fmt.Sprintf("%v", n.Value), n.Typex, nil
	case ast.TypeString:
		return fmt.Sprintf("%q", n.Value), n.Typex, nil
	default:
		return "", ast.TypeInvalid, errors.Errorf("Unexpected literal type %v", n.Typex)
	}
}

func (w *nodeHILWalker) walkOutput(n *ast.Output) (string, ast.Type, error) {
	strs, typs, err := w.walkNodes(n.Exprs)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

	if len(typs) == 1 {
		return strs[0], typs[0], nil
	}

	buf := &bytes.Buffer{}
	for i, s := range strs {
		if i > 0 {
			fmt.Fprintf(buf, " + ")
		}
		if typs[i] == ast.TypeString {
			fmt.Fprintf(buf, "%s", s)
		} else {
			fmt.Fprintf(buf, "`${%s}`", s)
		}
	}
	return buf.String(), ast.TypeString, nil
}

func (w *nodeHILWalker) walkVariableAccess(n *ast.VariableAccess) (string, ast.Type, error) {
	tfVar, err := config.NewInterpolatedVariable(n.Name)
	if err != nil {
		return "", ast.TypeInvalid, err
	}

	switch v := tfVar.(type) {
	case *config.CountVariable:
		// "count."
		if v.Type != config.CountValueIndex {
			return "", ast.TypeInvalid, errors.Errorf("unsupported count variable %s", v.FullKey())
		}

		if w.pc.countIndex == "" {
			return "", ast.TypeInvalid, errors.Errorf("no count index in scope")
		}

		return w.pc.countIndex, ast.TypeFloat, nil
	case *config.LocalVariable:
		// "local."
		return "", ast.TypeInvalid, errors.New("NYI: local variables")
	case *config.ModuleVariable:
		// "module."
		return "", ast.TypeInvalid, errors.New("NYI: module variables")
	case *config.PathVariable:
		// "path."
		return "", ast.TypeInvalid, errors.New("NYI: path variables")
	case *config.ResourceVariable:
		// default

		// look up the resource.
		r, ok := w.pc.g.graph.Resources[v.ResourceId()]
		if !ok {
			return "", ast.TypeInvalid, errors.Errorf("unknown resource %v", v.ResourceId())
		}

		var resInfo *tfbridge.ResourceInfo
		var sch schemas
		if r.Provider.Info != nil {
			resInfo = r.Provider.Info.Resources[v.Type]
			sch.tfRes = r.Provider.Info.P.ResourcesMap[v.Type]
			sch.pulumi = &tfbridge.SchemaInfo{Fields: resInfo.Fields}
		}

		// name{.property}+
		elements := strings.Split(v.Field, ".")
		for i, e := range elements {
			sch = sch.propertySchemas(e)
			elements[i] = tfbridge.TerraformToPulumiName(e, sch.tf, false)
		}

		// Handle multi-references (splats and indexes)
		receiver := resName(v.Type, v.Name)
		accessor, exprType := strings.Join(elements, "."), sch.astType()
		if v.Multi {
			if v.Index == -1 {
				// Splat
				accessor, exprType = fmt.Sprintf("map(v => v.%s)", accessor), ast.TypeList
			} else {
				// Index
				receiver = fmt.Sprintf("%s[%d]", receiver, v.Index)
			}
		}

		return fmt.Sprintf("%s.%s", receiver, accessor), exprType, nil
	case *config.SelfVariable:
		// "self."
		return "", ast.TypeInvalid, errors.New("NYI: self variables")
	case *config.SimpleVariable:
		// "[^.]\+"
		return "", ast.TypeInvalid, errors.New("NYI: simple variables")
	case *config.TerraformVariable:
		// "terraform."
		return "", ast.TypeInvalid, errors.New("NYI: terraform variables")
	case *config.UserVariable:
		// "var."
		if v.Elem != "" {
			return "", ast.TypeInvalid, errors.New("NYI: user variable elements")
		}

		// look up the variable.
		vn, ok := w.pc.g.graph.Variables[v.Name]
		if !ok {
			return "", ast.TypeInvalid, errors.Errorf("unknown variable %s", v.Name)
		}

		// If the variable does not have a default, its type is string. If it does have a default, its type is string
		// iff the default's type is also string. Note that we don't try all that hard here.
		typ := ast.TypeString
		if vn.DefaultValue != nil {
			if _, ok := vn.DefaultValue.(string); !ok {
				typ = ast.TypeUnknown
			}
		}

		return tfbridge.TerraformToPulumiName(v.Name, nil, false), typ, nil
	default:
		return "", ast.TypeInvalid, errors.Errorf("unexpected variable type %T", v)
	}
}

func (w *nodeHILWalker) walkNode(n ast.Node) (string, ast.Type, error) {
	switch n := n.(type) {
	case *ast.Arithmetic:
		return w.walkArithmetic(n)
	case *ast.Call:
		return w.walkCall(n)
	case *ast.Conditional:
		return w.walkConditional(n)
	case *ast.Index:
		return w.walkIndex(n)
	case *ast.LiteralNode:
		return w.walkLiteral(n)
	case *ast.Output:
		return w.walkOutput(n)
	case *ast.VariableAccess:
		return w.walkVariableAccess(n)
	default:
		return "", ast.TypeInvalid, errors.Errorf("unexpected HIL node type %T", n)
	}
}

func (w *nodeHILWalker) walkNodes(ns []ast.Node) ([]string, []ast.Type, error) {
	strs, typs := make([]string, len(ns)), make([]ast.Type, len(ns))
	for i, n := range ns {
		s, t, err := w.walkNode(n)
		if err != nil {
			return nil, nil, err
		}
		strs[i], typs[i] = s, t
	}
	return strs, typs, nil
}

type nodePropertyComputer struct {
	g *nodeGenerator
	countIndex string
}

func (pc *nodePropertyComputer) computeHILProperty(n ast.Node) (string, ast.Type, error) {
	// NOTE: this will need to change in order to deal with combinations of resource outputs and other operators: most
	// translations will not be output-aware, so we'll need to transform things into applies.
	return (&nodeHILWalker{pc: pc}).walkNode(n)
}

func (pc *nodePropertyComputer) computeSliceProperty(s []interface{}, indent string, sch schemas) (string, ast.Type, error) {
	buf := &bytes.Buffer{}

	elemSch := sch.elemSchemas()
	if tfbridge.IsMaxItemsOne(sch.tf, sch.pulumi) {
		switch len(s) {
		case 0:
			return "undefined", ast.TypeUnknown, nil
		case 1:
			return pc.computeProperty(s[0], indent, elemSch)
		default:
			return "", ast.TypeInvalid, errors.Errorf("expected at most one item in list")
		}
	}

	fmt.Fprintf(buf, "[")
	for _, v := range s {
		elemIndent := indent + "    "
		elem, elemTyp, err := pc.computeProperty(v, elemIndent, elemSch)
		if err != nil {
			return "", ast.TypeInvalid, err
		}
		if elemTyp == ast.TypeList {
			// TF flattens list elements that are themselves lists into the parent list.
			//
			// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
			// only knowable at runtime and will require a helper.
			elem = "..." + elem
		}
		fmt.Fprintf(buf, "\n%s%s,", elemIndent, coerceProperty(elem, elemTyp, elemSch.astType()))
	}
	fmt.Fprintf(buf, "\n%s]", indent)
	return buf.String(), ast.TypeList, nil
}

func (pc *nodePropertyComputer) computeMapProperty(m map[string]interface{}, indent string, sch schemas) (string, ast.Type, error) {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "{")
	for _, k := range gen.SortedKeys(m) {
		v := m[k]

		propSch := sch.propertySchemas(k)

		propIndent := indent + "    "
		prop, propTyp, err := pc.computeProperty(v, propIndent, propSch)
		if err != nil {
			return "", ast.TypeInvalid, err
		}
		prop = coerceProperty(prop, propTyp, propSch.astType())

		fmt.Fprintf(buf, "\n%s%s: %s,", propIndent, tsName(k, propSch.tf, propSch.pulumi, true), prop)
	}
	fmt.Fprintf(buf, "\n%s}", indent)
	return buf.String(), ast.TypeMap, nil
}

func (pc *nodePropertyComputer) computeProperty(v interface{}, indent string, sch schemas) (string, ast.Type, error) {
	if node, ok := v.(ast.Node); ok {
		return pc.computeHILProperty(node)
	}

	refV := reflect.ValueOf(v)
	switch refV.Kind() {
	case reflect.Bool:
		return fmt.Sprintf("%v", v), ast.TypeBool, nil
	case reflect.Int, reflect.Float64:
		return fmt.Sprintf("%v", v), ast.TypeFloat, nil
	case reflect.String:
		return fmt.Sprintf("%q", v), ast.TypeString, nil
	case reflect.Slice:
		return pc.computeSliceProperty(v.([]interface{}), indent, sch)
	case reflect.Map:
		return pc.computeMapProperty(v.(map[string]interface{}), indent, sch)
	default:
		contract.Failf("unexpected property type %v", refV.Type())
		return "", ast.TypeInvalid, errors.New("unexpected property type")
	}
}

func (g *nodeGenerator) computeProperty(v interface{}, indent string, sch schemas) (string, ast.Type, error) {
	return (&nodePropertyComputer{g: g}).computeProperty(v, indent, sch)
}

func (g *nodeGenerator) computePropertyWithCount(v interface{}, indent string, sch schemas, count string) (string, ast.Type, error) {
	return (&nodePropertyComputer{g: g, countIndex: count}).computeProperty(v, indent, sch)
}

func (g *nodeGenerator) GeneratePreamble(gr *il.Graph) error {
	// Stash the graph for later.
	g.graph = gr

	// Emit imports for the various providers
	fmt.Printf("import * as pulumi from \"@pulumi/pulumi\";\n")
	for _, p := range gr.Providers {
		fmt.Printf("import * as %s from \"@pulumi/%s\";\n", p.Config.Name, p.Config.Name)
	}
	fmt.Printf("import * as fs from \"fs\";")
	fmt.Printf("\n\n")

	return nil
}

func (g *nodeGenerator) GenerateVariables(vs []*il.VariableNode) error {
	// If there are no variables, we're done.
	if len(vs) == 0 {
		return nil
	}

	// Otherwise, new up a config object and declare the various vars.
	fmt.Printf("const config = new pulumi.Config(\"%s\")\n", g.projectName)
	for _, v := range vs {
		name := tfbridge.TerraformToPulumiName(v.Config.Name, nil, false)

		fmt.Printf("const %s = ", name)
		if v.DefaultValue == nil {
			fmt.Printf("config.require(\"%s\")", name)
		} else {
			def, _, err := g.computeProperty(v.DefaultValue, "", schemas{})
			if err != nil {
				return err
			}

			fmt.Printf("config.get(\"%s\") || %s", name, def)
		}
		fmt.Printf(";\n")
	}
	fmt.Printf("\n")

	return nil
}

func (*nodeGenerator) GenerateLocal(l *il.LocalNode) error {
	return errors.New("NYI: locals")
}

func (g *nodeGenerator) GenerateResource(r *il.ResourceNode) error {
	config := r.Config

	underscore := strings.IndexRune(config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := config.Type[:underscore], config.Type[underscore+1:]

	var resInfo *tfbridge.ResourceInfo
	var sch schemas
	if r.Provider.Info != nil {
		resInfo = r.Provider.Info.Resources[config.Type]
		sch.tfRes = r.Provider.Info.P.ResourcesMap[config.Type]
		sch.pulumi = &tfbridge.SchemaInfo{Fields: resInfo.Fields}
	}

	typeName := tfbridge.TerraformToPulumiName(resourceType, nil, true)

	module := ""
	if resInfo != nil {
		components := strings.Split(string(resInfo.Tok), ":")
		if len(components) != 3 {
			return errors.Errorf("unexpected resource token format %s", resInfo.Tok)
		}

		mod, typ := components[1], components[2]

		slash := strings.IndexRune(mod, '/')
		if slash == -1 {
			return errors.Errorf("unexpected resource module format %s", mod)
		}

		module, typeName = "."+mod[:slash], typ
	}

	// Build the list of explicit deps, if any.
	explicitDeps := ""
	if len(r.ExplicitDeps) != 0 {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, ", {dependsOn: [")
		for i, n := range r.ExplicitDeps {
			if i > 0 {
				fmt.Fprintf(buf, ", ")
			}
			depRes := n.(*il.ResourceNode)
			if depRes.Count != nil {
				fmt.Fprintf(buf, "...")
			}
			fmt.Fprintf(buf, "%s", resName(depRes.Config.Type, depRes.Config.Name))
		}
		fmt.Fprintf(buf, "]}")
		explicitDeps = buf.String()
	}

	name := resName(config.Type, config.Name)
	qualifiedTypeName := fmt.Sprintf("%s%s.%s", provider, module, typeName)
	if r.Count == nil {
		// If count is nil, this is a single-instance resource.
		inputs, _, err := g.computeProperty(r.Properties, "", sch)
		if err != nil {
			return err
		}

		fmt.Printf("const %s = new %s(\"%s\", %s%s);\n", name, qualifiedTypeName, config.Name, inputs, explicitDeps)
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, _, err := g.computeProperty(r.Count, "", schemas{})
		if err != nil {
			return err
		}
		inputs, _, err := g.computePropertyWithCount(r.Properties, "    ", sch, "i")
		if err != nil {
			return err
		}

		fmt.Printf("const %s: %s[] = [];\n", name, qualifiedTypeName)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		fmt.Printf("    %s.push(new %s(`%s-${i}`, %s%s));\n", name, qualifiedTypeName, config.Name, inputs, explicitDeps)
		fmt.Printf("}\n")
	}

	return nil
}

func (g *nodeGenerator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) == 0 {
		return nil
	}

	fmt.Printf("\n")
	for _, o := range os {
		outputs, _, err := g.computeProperty(o.Value, "", schemas{})
		if err != nil {
			return err
		}

		fmt.Printf("export const %s = %s;\n", tsName(o.Config.Name, nil, nil, false), outputs)
	}
	return nil
}

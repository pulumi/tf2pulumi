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
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

// TODO:
// - project from array-of-1-element to element
// - type-driven conversions in general (strings -> numbers, esp. for config)

type nodeGenerator struct{
	projectName string
}

type schemas struct {
	tf map[string]*schema.Schema
	pulumi map[string]*tfbridge.SchemaInfo
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

type nodeHILWalker struct{}

func (w nodeHILWalker) walkArithmetic(n *ast.Arithmetic) (string, error) {
	strs, err := w.walkNodes(n.Exprs)
	if err != nil {
		return "", err
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

	return "(" + strings.Join(strs, " " + op + " ") + ")", nil
}

func (w nodeHILWalker) walkCall(n *ast.Call) (string, error) {
	strs, err := w.walkNodes(n.Args)
	if err != nil {
		return "", err
	}

	switch n.Func {
	case "file":
		return fmt.Sprintf("fs.readFileSync(%s, \"utf-8\")", strs[0]), nil
	case "lookup":
		lookup := fmt.Sprintf("(<any>%s)[%s]", strs[0], strs[1])
		if len(strs) == 3 {
			lookup += fmt.Sprintf(" || %s", strs[2])
		}
		return lookup, nil
	case "split":
		// TODO: the spread operator shouldn't be here. This is to make ["${array-thing}"] work, and should be done
		// when translating the array.
		return fmt.Sprintf("...%s.split(%s)", strs[1], strs[0]), nil
	default:
		return "", errors.Errorf("NYI: call to %s", n.Func)
	}
}

func (w nodeHILWalker) walkConditional(n *ast.Conditional) (string, error) {
	cond, err := w.walkNode(n.CondExpr)
	if err != nil {
		return "", err
	}
	t, err := w.walkNode(n.TrueExpr)
	if err != nil {
		return "", err
	}
	f, err := w.walkNode(n.FalseExpr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("(%s ? %s : %s)", cond, t, f), nil
}

func (w nodeHILWalker) walkIndex(n *ast.Index) (string, error) {
	target, err := w.walkNode(n.Target)
	if err != nil {
		return "", err
	}
	key, err := w.walkNode(n.Key)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s[%s]", target, key), nil
}

func (w nodeHILWalker) walkLiteral(n *ast.LiteralNode) (string, error) {
	switch n.Typex {
	case ast.TypeBool, ast.TypeInt, ast.TypeFloat:
		return fmt.Sprintf("%v", n.Value), nil
	case ast.TypeString:
		return fmt.Sprintf("%q", n.Value), nil
	default:
		return "", errors.Errorf("Unexpected literal type %v", n.Typex)
	}
}

func (w nodeHILWalker) walkOutput(n *ast.Output) (string, error) {
	strs, err := w.walkNodes(n.Exprs)
	if err != nil {
		return "", err
	}
	return strings.Join(strs, ""), nil
}

func (w nodeHILWalker) walkVariableAccess(n *ast.VariableAccess) (string, error) {
	tfVar, err := config.NewInterpolatedVariable(n.Name)
	if err != nil {
		return "", err
	}

	switch v := tfVar.(type) {
	case *config.CountVariable:
		// "count."
		return "", errors.New("NYI: count variables")
	case *config.LocalVariable:
		// "local."
		return "", errors.New("NYI: local variables")
	case *config.ModuleVariable:
		// "module."
		return "", errors.New("NYI: module variables")
	case *config.PathVariable:
		// "path."
		return "", errors.New("NYI: path variables")
	case *config.ResourceVariable:
		// default
		if v.Multi {
			return "", errors.New("NYI: multi-resource variables")
		}

		// name{.property}+
		elements := strings.Split(v.Field, ".")
		for i, e := range elements {
			// TODO: grab a schema for each element based on the resource type and module for pluralization info
			elements[i] = tfbridge.TerraformToPulumiName(e, nil, false)
		}
		return fmt.Sprintf("%s.%s", resName(v.Type, v.Name), strings.Join(elements, ".")), nil
	case *config.SelfVariable:
		// "self."
		return "", errors.New("NYI: self variables")
	case *config.SimpleVariable:
		// "[^.]\+"
		return "", errors.New("NYI: simple variables")
	case *config.TerraformVariable:
		// "terraform."
		return "", errors.New("NYI: terraform variables")
	case *config.UserVariable:
		// "var."
		if v.Elem != "" {
			return "", errors.New("NYI: user variable elements")
		}

		return tfbridge.TerraformToPulumiName(v.Name, nil, false), nil
	default:
		return "", errors.Errorf("unexpected variable type %T", v)
	}
}

func (w nodeHILWalker) walkNode(n ast.Node) (string, error) {
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
		return "", errors.Errorf("unexpected HIL node type %T", n)
	}
}

func (w nodeHILWalker) walkNodes(ns []ast.Node) ([]string, error) {
	strs := make([]string, len(ns))
	for i, n := range ns {
		s, err := w.walkNode(n)
		if err != nil {
			return nil, err
		}
		strs[i] = s
	}
	return strs, nil
}

func (g *nodeGenerator) computeHILProperty(n ast.Node) (string, error) {
	// NOTE: this will need to change in order to deal with combinations of resource outputs and other operators: most
	// translations will not be output-aware, so we'll need to transform things into applies.
	return nodeHILWalker{}.walkNode(n)
}

func (g *nodeGenerator) computeSliceProperty(s []interface{}, indent string, elemSch schemas) (string, error) {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "[")
	for _, v := range s {
		elemIndent := indent + "    "
		elem, err := g.computeProperty(v, elemIndent, elemSch)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "\n%s%s,", elemIndent, elem)
	}
	fmt.Fprintf(buf, "\n%s]", indent)
	return buf.String(), nil
}

func (g *nodeGenerator) computeMapProperty(m map[string]interface{}, indent string, sch schemas) (string, error) {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "{")
	for k, v := range m {
		var elemTf *schema.Schema
		if sch.tf != nil {
			elemTf = sch.tf[k]
		}

		elemSch := sch
		if elemTf != nil {
			if elemResource, ok := elemTf.Elem.(*schema.Resource); ok {
				elemSch.tf = elemResource.Schema
			}
		}

		var elemPulumi *tfbridge.SchemaInfo
		if sch.pulumi != nil {
			elemPulumi = sch.pulumi[k]
			if elemPulumi != nil {
				elemSch.pulumi = elemPulumi.Fields
			}
		}

		elemIndent := indent + "    "
		elem, err := g.computeProperty(v, elemIndent, elemSch)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "\n%s%s: %s,", elemIndent, tsName(k, elemTf, elemPulumi, true), elem)
	}
	fmt.Fprintf(buf, "\n%s}", indent)
	return buf.String(), nil
}

func (g *nodeGenerator) computeProperty(v interface{}, indent string, sch schemas) (string, error) {
	if node, ok := v.(ast.Node); ok {
		return g.computeHILProperty(node)
	}

	refV := reflect.ValueOf(v)
	switch refV.Kind() {
	case reflect.Bool, reflect.Int, reflect.Float64:
		return fmt.Sprintf("%v", v), nil
	case reflect.String:
		return fmt.Sprintf("%q", v), nil
	case reflect.Slice:
		return g.computeSliceProperty(v.([]interface{}), indent, sch)
	case reflect.Map:
		return g.computeMapProperty(v.(map[string]interface{}), indent, sch)
	default:
		contract.Failf("unexpected property type %v", refV.Type())
		return "", errors.New("unexpected property type")
	}
}

func (*nodeGenerator) generatePreamble(g *graph) error {
	// Emit imports for the various providers
	fmt.Printf("import * as pulumi from \"@pulumi/pulumi\";\n")
	for _, p := range g.providers {
		fmt.Printf("import * as %s from \"@pulumi/%s\";\n", p.config.Name, p.config.Name)
	}
	fmt.Printf("import * as fs from \"fs\";")
	fmt.Printf("\n\n")

	return nil
}

func (g *nodeGenerator) generateVariables(vs []*variableNode) error {
	// If there are no variables, we're done.
	if len(vs) == 0 {
		return nil
	}

	// Otherwise, new up a config object and declare the various vars.
	fmt.Printf("const config = new pulumi.Config(\"%s\")\n", g.projectName)
	for _, v := range vs {
		name := tfbridge.TerraformToPulumiName(v.config.Name, nil, false)

		fmt.Printf("const %s = ", name)
		if v.defaultValue == nil {
			fmt.Printf("config.require(\"%s\")", name)
		} else {
			def, err := g.computeProperty(v.defaultValue, "", schemas{})
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

func (*nodeGenerator) generateLocal(l *localNode) error {
	return errors.New("NYI: locals")
}

func (g *nodeGenerator) generateResource(r *resourceNode) error {
	config := r.config

	underscore := strings.IndexRune(config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := config.Type[:underscore], config.Type[underscore+1:]

	var resInfo *tfbridge.ResourceInfo
	var sch schemas
	if r.provider.info != nil {
		resInfo = r.provider.info.Resources[config.Type]
		sch.tf = r.provider.info.P.ResourcesMap[config.Type].Schema
		sch.pulumi = resInfo.Fields
	}

	inputs, err := g.computeProperty(r.properties, "", sch)
	if err != nil {
		return err
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

		module, typeName = "." + mod[:slash], typ
	}

	fmt.Printf("const %s = new %s%s.%s(\"%s\", %s",
		resName(config.Type, config.Name), provider, module, typeName, config.Name, inputs)

	if len(r.explicitDeps) != 0 {
		fmt.Printf(", {dependsOn: [")
		for i, n := range r.explicitDeps {
			if i > 0 {
				fmt.Printf(", ")
			}
			r := n.(*resourceNode)
			fmt.Printf("%s", resName(r.config.Type, r.config.Name))
		}
		fmt.Printf("]}")
	}

	fmt.Printf(");\n")

	return nil
}

func (g *nodeGenerator) generateOutputs(os []*outputNode) error {
	if len(os) == 0 {
		return nil
	}

	fmt.Printf("\n")
	for _, o := range os {
		outputs, err := g.computeProperty(o.value, "", schemas{})
		if err != nil {
			return err
		}

		fmt.Printf("export const %s = %s;\n", tsName(o.config.Name, nil, nil, false), outputs)
	}
	return nil
}


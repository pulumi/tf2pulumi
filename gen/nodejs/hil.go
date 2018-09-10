package nodejs

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

// This file contains the code necessary to generate code for bound expression trees. It is the responsibility of each
// node-specific generation function to ensure that the generated code is appropriately parenthesized where necessary
// in order to avoid unexpected issues with operator precedence.

// genArithmetic generates code for the given arithmetic expression.
func (g *Generator) genArithmetic(w io.Writer, n *il.BoundArithmetic) {
	op := ""
	switch n.HILNode.Op {
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
	op = fmt.Sprintf(" %s ", op)

	g.gen(w, "(")
	for i, n := range n.Exprs {
		if i != 0 {
			g.gen(w, op)
		}
		g.gen(w, n)
	}
	g.gen(w, ")")
}

// genApplyOutput generates code for a single argument to a `.apply` invocation.
func (g *Generator) genApplyOutput(w io.Writer, n *il.BoundVariableAccess) {
	if rv, ok := n.TFVar.(*config.ResourceVariable); ok && rv.Multi && rv.Index == -1 {
		g.genf(w, "pulumi.all(%v)", n)
	} else {
		g.gen(w, n)
	}
}

// genApply generates code for a single `.apply` invocation as represented by a call to the `__apply` intrinsic.
func (g *Generator) genApply(w io.Writer, n *il.BoundCall) {
	// Extract the list of outputs and the continuation expression from the `__apply` arguments.
	g.applyArgs = n.Args[:len(n.Args)-1]
	then := n.Args[len(n.Args)-1]

	if len(g.applyArgs) == 1 {
		// If we only have a single output, just generate a normal `.apply`.
		g.genApplyOutput(w, g.applyArgs[0].(*il.BoundVariableAccess))
		g.genf(w, ".apply(__arg0 => %v)", then)
	} else {
		// Otherwise, generate a call to `pulumi.all([]).apply()`.
		g.gen(w, "pulumi.all([")
		for i, o := range g.applyArgs {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.genApplyOutput(w, o.(*il.BoundVariableAccess))
		}
		g.gen(w, "]).apply(([")
		for i := range g.applyArgs {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.genf(w, "__arg%d", i)
		}
		g.gen(w, "]) => ", then, ")")
	}

	// Finally, clear the current set of apply arguments.
	g.applyArgs = nil
}

// genApplyArg generates a single refernce to a resolved output value inside the context of a call top `.apply`.
func (g *Generator) genApplyArg(w io.Writer, index int) {
	contract.Assert(g.applyArgs != nil)

	// Extract the variable reference.
	v := g.applyArgs[index].(*il.BoundVariableAccess)

	// Generate a reference to the parameter.
	g.genf(w, "__arg%d", index)

	// Generate any nested path.
	if rv, ok := v.TFVar.(*config.ResourceVariable); ok {
		// Handle splats
		isSplat := rv.Multi && rv.Index == -1
		if isSplat {
			g.gen(w, ".map(v => v")
		}

		r := v.ILNode.(*il.ResourceNode)
		sch, elements := v.Schemas, v.Elements
		if r.Config.Mode == config.ManagedResourceMode {
			sch, elements = sch.PropertySchemas(elements[0]), elements[1:]
		} else if r.Provider.Config.Name == "http" {
			elements = nil
		}
		for _, e := range elements {
			isListElement := sch.Type().IsList()
			projectListElement := isListElement && tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi)

			sch = sch.PropertySchemas(e)
			if isListElement {
				// If we're projecting the list element, just skip this path element entirely.
				if !projectListElement {
					g.genf(w, "[%s]", e)
				}
			} else {
				g.genf(w, ".%s", tfbridge.TerraformToPulumiName(e, sch.TF, false))
			}
		}

		if isSplat {
			g.gen(w, ")")
		}
	}
}

// genCoercion generates code for a single call to the __coerce intrinsic that converts an expression between types.
func (g *Generator) genCoercion(w io.Writer, n il.BoundExpr, toType il.Type) {
	switch n.Type() {
	case il.TypeBool:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%v\"", lit)
			} else {
				g.genf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeNumber:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%v\"", lit)
			} else {
				g.genf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeString:
		switch toType {
		case il.TypeBool:
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "%v", lit.Value.(string) == "true")
			} else {
				g.genf(w, "(%v === \"true\")", n)
			}
			return
		case il.TypeNumber:
			g.genf(w, "Number.parseFloat(%v)", n)
			return
		}
	}

	// If we get here, we weren't able to genereate a coercion. Just generate the node. This is questionable behavior
	// at best.
	g.gen(w, n)
}

// genCall generates code for a call expression.
func (g *Generator) genCall(w io.Writer, n *il.BoundCall) {
	switch n.HILNode.Func {
	case "__apply":
		g.genApply(w, n)
	case "__applyArg":
		g.genApplyArg(w, n.Args[0].(*il.BoundLiteral).Value.(int))
	case "__archive":
		g.genf(w, "new pulumi.asset.FileArchive(%v)", n.Args[0])
	case "__asset":
		g.genf(w, "new pulumi.asset.FileAsset(%v)", n.Args[0])
	case "__coerce":
		g.genCoercion(w, n.Args[0], n.Type())
	case "base64decode":
		g.genf(w, "Buffer.from(%v, \"base64\").toString()", n.Args[0])
	case "base64encode":
		g.genf(w, "Buffer.from(%v).toString(\"base64\")", n.Args[0])
	case "chomp":
		g.genf(w, "%v.replace(/(\\n|\\r\\n)*$/, \"\")", n.Args[0])
	case "coalesce":
		g.gen(w, "[")
		for i, v := range n.Args {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.gen(w, v)
		}
		g.gen(w, "].find((v: any) => v !== undefined && v !== \"\")")
	case "coalescelist":
		g.gen(w, "[")
		for i, v := range n.Args {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.gen(w, v)
		}
		g.gen(w, "].find((v: any) => v !== undefined && (<any[]>v).length > 0)")
	case "compact":
		g.genf(w, "%v.filter((v: any) => <string>v !== \"\")", n.Args[0])
	case "element":
		g.genf(w, "%v[%v]", n.Args[0], n.Args[1])
	case "file":
		g.genf(w, "fs.readFileSync(%v, \"utf-8\")", n.Args[0])
	case "format":
		g.gen(w, "sprintf.sprintf(")
		for i, a := range n.Args {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.gen(w, a)
		}
		g.gen(w, ")")
	case "join":
		g.genf(w, "%v.join(%v)", n.Args[1], n.Args[0])
	case "length":
		g.genf(w, "%v.length", n.Args[0])
	case "list":
		g.gen(w, "[")
		for i, e := range n.Args {
			if i > 0 {
				g.gen(w, ", ")
			}
			g.gen(w, e)
		}
		g.gen(w, "]")
	case "lookup":
		hasDefault := len(n.Args) == 3
		if hasDefault {
			g.gen(w, "(")
		}
		g.genf(w, "(<any>%v)[%v]", n.Args[0], n.Args[1])
		if hasDefault {
			g.genf(w, " || %v)", n.Args[2])
		}
	case "lower":
		g.genf(w, "%v.toLowerCase()", n.Args[0])
	case "map":
		contract.Assert(len(n.Args)%2 == 0)
		g.gen(w, "{")
		for i := 0; i < len(n.Args); i += 2 {
			if i > 0 {
				g.gen(w, ", ")
			}
			if lit, ok := n.Args[i].(*il.BoundLiteral); ok && lit.Type() == il.TypeString {
				g.gen(w, lit)
			} else {
				g.genf(w, "[%v]", n.Args[i])
			}
			g.genf(w, ": %v", n.Args[i+1])
		}
		g.gen(w, "}")
	case "replace":
		pat := (interface{})(n.Args[1])
		if lit, ok := pat.(*il.BoundLiteral); ok && lit.Type() == il.TypeString {
			patStr := lit.Value.(string)
			if len(patStr) > 1 && patStr[0] == '/' && patStr[1] == '/' {
				pat = patStr
			}
		}
		g.genf(w, "%v.replace(%v, %v)", n.Args[0], pat, n.Args[2])
	case "split":
		g.genf(w, "%v.split(%v)", n.Args[1], n.Args[0])
	case "zipmap":
		g.genf(w, "((keys, values) => Object.assign.apply({}, keys.map((k: any, i: number) => ({[k]: values[i]}))))(%v, %v)",
			n.Args[0], n.Args[1])
	default:
		g.genf(w, "(() => { throw \"NYI: call to %v\"; })()", n.HILNode.Func)
	}
}

// genConditional generates code for a single conditional expression.
func (g *Generator) genConditional(w io.Writer, n *il.BoundConditional) {
	g.genf(w, "(%v ? %v : %v)", n.CondExpr, n.TrueExpr, n.FalseExpr)
}

// genIndex generates code for a single index expression.
func (g *Generator) genIndex(w io.Writer, n *il.BoundIndex) {
	g.genf(w, "%v[%v]", n.TargetExpr, n.KeyExpr)
}

// genLiteral generates code for a single literal expression
func (g *Generator) genLiteral(w io.Writer, n *il.BoundLiteral) {
	switch n.ExprType {
	case il.TypeBool:
		g.genf(w, "%v", n.Value)
	case il.TypeNumber:
		f := n.Value.(float64)
		if float64(int64(f)) == f {
			g.genf(w, "%d", int64(f))
		} else {
			g.genf(w, "%f", n.Value)
		}
	case il.TypeString:
		g.genf(w, "%q", n.Value)
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", n.ExprType)
	}
}

// genOutput generates code for a single output expression.
func (g *Generator) genOutput(w io.Writer, n *il.BoundOutput) {
	g.gen(w, "`")
	for _, s := range n.Exprs {
		if lit, ok := s.(*il.BoundLiteral); ok && lit.ExprType == il.TypeString {
			g.gen(w, lit.Value.(string))
		} else {
			g.genf(w, "${%v}", s)
		}
	}
	g.gen(w, "`")
}

// genVariableAccess generates code for a single variable access expression.
func (g *Generator) genVariableAccess(w io.Writer, n *il.BoundVariableAccess) {
	switch v := n.TFVar.(type) {
	case *config.CountVariable:
		g.gen(w, g.countIndex)
	case *config.LocalVariable:
		g.gen(w, localName(v.Name))
	case *config.ModuleVariable:
		g.genf(w, "mod_%s", cleanName(v.Name))
		for _, e := range strings.Split(v.Field, ".") {
			g.genf(w, ".%s", tfbridge.TerraformToPulumiName(e, nil, false))
		}
	case *config.PathVariable:
		switch v.Type {
		case config.PathValueCwd:
			g.gen(w, "process.cwd()")
		case config.PathValueModule:
			path := g.module.Tree.Config().Dir

			// Attempt to make this path relative to that of the root module.
			rel, err := filepath.Rel(g.rootPath, path)
			if err == nil {
				path = rel
			}

			g.gen(w, &il.BoundLiteral{ExprType: il.TypeString, Value: path})
		case config.PathValueRoot:
			// NOTE: this might not be the most useful or correct value. Might want Node's __directory or similar.
			g.gen(w, &il.BoundLiteral{ExprType: il.TypeString, Value: "."})
		}
	case *config.ResourceVariable:
		r := n.ILNode.(*il.ResourceNode)

		// We only generate up to the "output" part of the path here: the apply transform will take care of the rest.
		g.gen(w, resName(v.Type, v.Name))
		if v.Multi && v.Index != -1 {
			g.genf(w, "[%d]", v.Index)
		}

		// A managed resource is not itself an output: instead, it is a bag of outputs. As such, we must generate the
		// first portion of this access.
		if r.Config.Mode == config.ManagedResourceMode && len(n.Elements) > 0 {
			element := n.Elements[0]
			elementSch := n.Schemas.PropertySchemas(element)

			// Handle splats
			isSplat := v.Multi && v.Index == -1
			if isSplat {
				g.gen(w, ".map(v => v")
			}
			g.genf(w, ".%s", tfbridge.TerraformToPulumiName(element, elementSch.TF, false))
			if isSplat {
				g.gen(w, ")")
			}
		}
	case *config.UserVariable:
		g.gen(w, "var_", cleanName(v.Name))
	default:
		contract.Failf("unexpected TF var type in genVariableAccess: %T", n.TFVar)
	}
}

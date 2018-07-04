package nodejs

import (
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"

	"github.com/pgavlin/firewalker/il"
)

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

func (g *Generator) genApplyOutput(w io.Writer, n *il.BoundVariableAccess) {
	if rv, ok := n.TFVar.(*config.ResourceVariable); ok && rv.Multi && rv.Index == -1 {
		g.genf(w, "pulumi.all(%v)", n)
	} else {
		g.gen(w, n)
	}
}

func (g *Generator) genApply(w io.Writer, n *il.BoundCall) {
	g.applyArgs = n.Args[:len(n.Args)-1]
	then := n.Args[len(n.Args)-1]

	if len(g.applyArgs) == 1 {
		g.genApplyOutput(w, g.applyArgs[0].(*il.BoundVariableAccess))
		g.genf(w, ".apply(__arg0 => %v)", then)
	} else {
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

	g.applyArgs = nil
}

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

func (g *Generator) genCoercion(w io.Writer, n il.BoundExpr, toType il.Type) {
	switch n.Type() {
	case il.TypeBool:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%v\"", lit.Value)
			} else {
				g.genf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeNumber:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%f\"", lit.Value)
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

func (g *Generator) genCall(w io.Writer, n *il.BoundCall) {
	switch n.HILNode.Func {
	case "__apply":
		g.genApply(w, n)
	case "__applyArg":
		g.genApplyArg(w, n.Args[0].(*il.BoundLiteral).Value.(int))
	case "__archive":
		arg := n.Args[0]
		if v, ok := arg.(*il.BoundVariableAccess); ok {
			if r, ok := v.ILNode.(*il.ResourceNode); ok && r.Provider.Config.Name == "archive" {
				// Just generate a reference to the asset itself.
				g.gen(w, resName(r.Config.Type, r.Config.Name))
				return
			}
		}

		g.genf(w, "new pulumi.asset.FileArchive(%v)", arg)
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
	case "element":
		g.genf(w, "%v[%v]", n.Args[0], n.Args[1])
	case "file":
		g.genf(w, "fs.readFileSync(%v, \"utf-8\")", n.Args[0])
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
	case "map":
		contract.Assert(len(n.Args) % 2 == 0)
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
	case "split":
		g.genf(w, "%v.split(%v)", n.Args[1], n.Args[0])
	default:
		contract.Failf("unexpected function in genCall: %v", n.HILNode.Func)
	}
}

func (g *Generator) genConditional(w io.Writer, n *il.BoundConditional) {
	g.genf(w, "(%v ? %v : %v)", n.CondExpr, n.TrueExpr, n.FalseExpr)
}

func (g *Generator) genIndex(w io.Writer, n *il.BoundIndex) {
	g.genf(w, "%v[%v]", n.TargetExpr, n.KeyExpr)
}

func (g *Generator) genLiteral(w io.Writer, n *il.BoundLiteral) {
	switch n.ExprType {
	case il.TypeBool, il.TypeNumber:
		g.genf(w, "%v", n.Value)
	case il.TypeString:
		g.genf(w, "%q", n.Value)
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", n.ExprType)
	}
}

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
		g.gen(w, tsName(v.Name, nil, nil, false))
	default:
		contract.Failf("unexpected TF var type in genVariableAccess: %T", n.TFVar)
	}
}

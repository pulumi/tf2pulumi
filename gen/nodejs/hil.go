package nodejs

import (
	"bytes"
	"fmt"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/il"
)

type hilGenerator struct {
	w          *bytes.Buffer
	countIndex string
	applyArgs  []il.BoundExpr
}

func (g *hilGenerator) genArithmetic(n *il.BoundArithmetic) {
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

	g.gen("(")
	for i, n := range n.Exprs {
		if i != 0 {
			g.gen(op)
		}
		g.gen(n)
	}
	g.gen(")")
}

func (g *hilGenerator) genApplyOutput(n *il.BoundVariableAccess) {
	rv := n.TFVar.(*config.ResourceVariable)

	if rv.Multi && rv.Index == -1 {
		g.gen("pulumi.all(", n, ")")
	} else {
		g.gen(n)
	}
}

func (g *hilGenerator) genApply(n *il.BoundCall) {
	g.applyArgs = n.Args[:len(n.Args)-1]
	then := n.Args[len(n.Args)-1]

	if len(g.applyArgs) == 1 {
		g.genApplyOutput(g.applyArgs[0].(*il.BoundVariableAccess))
		g.gen(".apply(__arg0 => ", then, ")")
	} else {
		g.gen("pulumi.all([")
		for i, o := range g.applyArgs {
			if i > 0 {
				g.gen(", ")
			}
			g.genApplyOutput(o.(*il.BoundVariableAccess))
		}
		g.gen("]).apply(([")
		for i := range g.applyArgs {
			if i > 0 {
				g.gen(", ")
			}
			g.gen(fmt.Sprintf("__arg%d", i))
		}
		g.gen("]) => ", then, ")")
	}

	g.applyArgs = nil
}

func (g *hilGenerator) genApplyArg(index int) {
	contract.Assert(g.applyArgs != nil)

	// Extract the variable reference.
	v := g.applyArgs[index].(*il.BoundVariableAccess)

	// Generate a reference to the parameter.
	g.gen(fmt.Sprintf("__arg%d", index))

	// Handle splats
	rv := v.TFVar.(*config.ResourceVariable)
	isSplat := rv.Multi && rv.Index == -1
	if isSplat {
		g.gen(".map(v => v")
	}

	// Generate any nested path.
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
				g.gen(fmt.Sprintf("[%s]", e))
			}
		} else {
			g.gen(fmt.Sprintf(".%s", tfbridge.TerraformToPulumiName(e, sch.TF, false)))
		}
	}

	if isSplat {
		g.gen(")")
	}
}

func (g *hilGenerator) genCall(n *il.BoundCall) {
	switch n.HILNode.Func {
	case "__apply":
		g.genApply(n)
	case "__applyArg":
		g.genApplyArg(n.Args[0].(*il.BoundLiteral).Value.(int))
	case "__archive":
		arg := n.Args[0]
		if v, ok := arg.(*il.BoundVariableAccess); ok {
			if r, ok := v.ILNode.(*il.ResourceNode); ok && r.Provider.Config.Name == "archive" {
				// Just generate a reference to the asset itself.
				g.gen(resName(r.Config.Type, r.Config.Name))
				return
			}
		}

		g.gen("new pulumi.asset.FileArchive(", arg, ")")
	case "__asset":
		g.gen("new pulumi.asset.FileAsset(", n.Args[0], ")")
	case "base64decode":
		g.gen("Buffer.from(", n.Args[0], ", \"base64\").toString()")
	case "base64encode":
		g.gen("Buffer.from(", n.Args[0], ").toString(\"base64\")")
	case "chomp":
		g.gen(n.Args[0], ".replace(/(\\n|\\r\\n)*$/, \"\")")
	case "element":
		g.gen(n.Args[0], "[", n.Args[1], "]")
	case "file":
		g.gen("fs.readFileSync(", n.Args[0], ", \"utf-8\")")
	case "list":
		g.gen("[")
		for i, e := range n.Args {
			if i > 0 {
				g.gen(", ")
			}
			g.gen(e)
		}
		g.gen("]")
	case "lookup":
		hasDefault := len(n.Args) == 3
		if hasDefault {
			g.gen("(")
		}
		g.gen("(<any>", n.Args[0], ")[", n.Args[1], "]")
		if hasDefault {
			g.gen(" || ", n.Args[2], ")")
		}
	case "map":
		contract.Assert(len(n.Args) % 2 == 0)
		g.gen("{")
		for i := 0; i < len(n.Args); i += 2 {
			if i > 0 {
				g.gen(", ")
			}
			if lit, ok := n.Args[i].(*il.BoundLiteral); ok && lit.Type() == il.TypeString {
				g.gen(lit)
			} else {
				g.gen("[", n.Args[i], "]")
			}
			g.gen(": ", n.Args[i+1])
		}
		g.gen("}")
	case "split":
		g.gen(n.Args[1], ".split(", n.Args[0], ")")
	default:
		contract.Failf("unexpected function in genCall: %v", n.HILNode.Func)
	}
}

func (g *hilGenerator) genConditional(n *il.BoundConditional) {
	g.gen("(", n.CondExpr, " ? ", n.TrueExpr, " : ", n.FalseExpr, ")")
}

func (g *hilGenerator) genIndex(n *il.BoundIndex) {
	g.gen(n.TargetExpr, "[", n.KeyExpr, "]")
}

func (g *hilGenerator) genLiteral(n *il.BoundLiteral) {
	switch n.ExprType {
	case il.TypeBool, il.TypeNumber:
		fmt.Fprintf(g.w, "%v", n.Value)
	case il.TypeString:
		fmt.Fprintf(g.w, "%q", n.Value)
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", n.ExprType)
	}
}

func (g *hilGenerator) genOutput(n *il.BoundOutput) {
	g.gen("`")
	for _, s := range n.Exprs {
		if lit, ok := s.(*il.BoundLiteral); ok && lit.ExprType == il.TypeString {
			g.gen(lit.Value.(string))
		} else {
			g.gen("${", s, "}")
		}
	}
	g.gen("`")
}

func (g *hilGenerator) genVariableAccess(n *il.BoundVariableAccess) {
	switch v := n.TFVar.(type) {
	case *config.CountVariable:
		g.gen(g.countIndex)
	case *config.LocalVariable:
		g.gen(localName(v.Name))
	case *config.ResourceVariable:
		r := n.ILNode.(*il.ResourceNode)

		// We only generate up to the "output" part of the path here: the apply transform will take care of the rest.
		g.gen(resName(v.Type, v.Name))
		if v.Multi && v.Index != -1 {
			g.gen(fmt.Sprintf("[%d]", v.Index))
		}

		// A managed resource is not itself an output: instead, it is a bag of outputs. As such, we must generate the
		// first portion of this access.
		if r.Config.Mode == config.ManagedResourceMode && len(n.Elements) > 0 {
			element := n.Elements[0]
			elementSch := n.Schemas.PropertySchemas(element)

			// Handle splats
			isSplat := v.Multi && v.Index == -1
			if isSplat {
				g.gen(".map(v => v")
			}
			g.gen(fmt.Sprintf(".%s", tfbridge.TerraformToPulumiName(element, elementSch.TF, false)))
			if isSplat {
				g.gen(")")
			}
		}
	case *config.UserVariable:
		g.gen(tsName(v.Name, nil, nil, false))
	default:
		contract.Failf("unexpected TF var type in genVariableAccess: %T", n.TFVar)
	}
}

func (g *hilGenerator) gen(vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			g.w.WriteString(v)
		case *il.BoundArithmetic:
			g.genArithmetic(v)
		case *il.BoundCall:
			g.genCall(v)
		case *il.BoundConditional:
			g.genConditional(v)
		case *il.BoundIndex:
			g.genIndex(v)
		case *il.BoundLiteral:
			g.genLiteral(v)
		case *il.BoundOutput:
			g.genOutput(v)
		case *il.BoundVariableAccess:
			g.genVariableAccess(v)
		default:
			contract.Failf("unexpected type in gen: %T", v)
		}
	}
}

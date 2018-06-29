package nodejs

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/il"
)

type hilGenerator struct {
	w          *bytes.Buffer
	countIndex string
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

func (g *hilGenerator) genApplyArg(n *il.BoundVariableAccess) {
	rv := n.TFVar.(*config.ResourceVariable)

	if !rv.Multi {
		g.gen(n)
	} else {
		g.gen("pulumi.all(", n, ")")
	}
}

func (g *hilGenerator) genApply(n *il.BoundCall) {
	outputs := n.Args[:len(n.Args)-1]
	then := n.Args[len(n.Args)-1]

	if len(outputs) == 1 {
		g.genApplyArg(outputs[0].(*il.BoundVariableAccess))
		g.gen(".apply(__arg0 => ", then, ")")
	} else {
		g.gen("pulumi.all([")
		for i, o := range outputs {
			if i > 0 {
				g.gen(", ")
			}
			g.genApplyArg(o.(*il.BoundVariableAccess))
		}
		g.gen("]).apply(([")
		for i := range outputs {
			if i > 0 {
				g.gen(", ")
			}
			g.gen(fmt.Sprintf("__arg%d", i))
		}
		g.gen("]) => ", then, ")")
	}
}

func (g *hilGenerator) genCall(n *il.BoundCall) {
	switch n.HILNode.Func {
	case "__apply":
		g.genApply(n)
	case "__applyArg":
		g.gen(fmt.Sprintf("__arg%d", n.Args[0].(*il.BoundLiteral).Value.(int)))
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
			g.gen(n.Args[i], ": ", n.Args[i+1])
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
	case *config.ResourceVariable:
		r := n.ILNode.(*il.ResourceNode)
		if r.Provider.Config.Name == "http" {
			g.gen(resName(v.Type, v.Name))
			return
		}

		isDataSource := r.Config.Mode == config.DataResourceMode

		elements, elemSch := make([]string, len(n.Elements)), n.Schemas
		for i, e := range n.Elements {
			elemSch = elemSch.PropertySchemas(e)
			elements[i] = tfbridge.TerraformToPulumiName(e, elemSch.TF, false)
		}

		receiver, accessor := resName(v.Type, v.Name), strings.Join(elements, ".")
		if v.Multi {
			if v.Index == -1 {
				selector := fmt.Sprintf("v => v.%s", accessor)
				if isDataSource {
					selector = fmt.Sprintf("v => v.apply(%s)", selector)
				}
				accessor = fmt.Sprintf("map(%s)", selector)
			} else {
				receiver = fmt.Sprintf("%s[%d]", receiver, v.Index)
			}
		}
		if isDataSource {
			accessor = fmt.Sprintf("apply(v => v.%s)", accessor)
		}

		g.gen(receiver, ".", accessor)
	case *config.UserVariable:
		g.gen(tfbridge.TerraformToPulumiName(v.Name, nil, false))
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

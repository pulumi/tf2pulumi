package nodejs

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type hilWalker struct {
	pc *propertyComputer
}

func (w *hilWalker) walkArithmetic(n *ast.Arithmetic) (string, ast.Type, error) {
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

func (w *hilWalker) walkCall(n *ast.Call) (string, ast.Type, error) {
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

func (w *hilWalker) walkConditional(n *ast.Conditional) (string, ast.Type, error) {
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

func (w *hilWalker) walkIndex(n *ast.Index) (string, ast.Type, error) {
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

func (w *hilWalker) walkLiteral(n *ast.LiteralNode) (string, ast.Type, error) {
	switch n.Typex {
	case ast.TypeBool, ast.TypeInt, ast.TypeFloat:
		return fmt.Sprintf("%v", n.Value), n.Typex, nil
	case ast.TypeString:
		return fmt.Sprintf("%q", n.Value), n.Typex, nil
	default:
		return "", ast.TypeInvalid, errors.Errorf("Unexpected literal type %v", n.Typex)
	}
}

func (w *hilWalker) walkOutput(n *ast.Output) (string, ast.Type, error) {
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

func (w *hilWalker) walkVariableAccess(n *ast.VariableAccess) (string, ast.Type, error) {
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

func (w *hilWalker) walkNode(n ast.Node) (string, ast.Type, error) {
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

func (w *hilWalker) walkNodes(ns []ast.Node) ([]string, []ast.Type, error) {
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


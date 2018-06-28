package il

import (
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

func (b *propertyBinder) bindArithmetic(n *ast.Arithmetic) (BoundExpr, error) {
	exprs, err := b.bindExprs(n.Exprs)
	if err != nil {
		return nil, err
	}

	return &BoundArithmetic{HILNode: n, Exprs: exprs}, nil
}

func (b *propertyBinder) bindCall(n *ast.Call) (BoundExpr, error) {
	args, err := b.bindExprs(n.Args)
	if err != nil {
		return nil, err
	}

	exprType := TypeUnknown
	switch n.Func {
	case "element":
		if args[0].Type().IsList() {
			exprType = args[0].Type().ElementType()
		}
	case "lookup":
		// nothing to do
	case "file":
		exprType = TypeString
	case "split":
		exprType = TypeList
	default:
		return nil, errors.Errorf("NYI: call to %s", n.Func)
	}

	return &BoundCall{HILNode: n, ExprType: exprType, Args: args}, nil
}

func (b *propertyBinder) bindConditional(n *ast.Conditional) (BoundExpr, error) {
	condExpr, err := b.bindExpr(n.CondExpr)
	if err != nil {
		return nil, err
	}
	trueExpr, err := b.bindExpr(n.TrueExpr)
	if err != nil {
		return nil, err
	}
	falseExpr, err := b.bindExpr(n.FalseExpr)
	if err != nil {
		return nil, err
	}

	exprType := trueExpr.Type()
	if exprType != falseExpr.Type() {
		exprType = TypeUnknown
	}

	return &BoundConditional{
		HILNode:   n,
		ExprType:  exprType,
		CondExpr:  condExpr,
		TrueExpr:  trueExpr,
		FalseExpr: falseExpr,
	}, nil
}

func (b *propertyBinder) bindIndex(n *ast.Index) (BoundExpr, error) {
	boundTarget, err := b.bindExpr(n.Target)
	if err != nil {
		return nil, err
	}
	boundKey, err := b.bindExpr(n.Key)
	if err != nil {
		return nil, err
	}

	exprType := TypeUnknown
	targetType := boundTarget.Type()
	if targetType.IsList() {
		exprType = targetType.ElementType()
	}

	return &BoundIndex{
		HILNode:    n,
		ExprType:   exprType,
		TargetExpr: boundTarget,
		KeyExpr:    boundKey,
	}, nil
}

func (b *propertyBinder) bindLiteral(n *ast.LiteralNode) (BoundExpr, error) {
	exprType := TypeUnknown
	switch n.Typex {
	case ast.TypeBool:
		exprType = TypeBool
	case ast.TypeInt, ast.TypeFloat:
		exprType = TypeNumber
	case ast.TypeString:
		exprType = TypeString
	default:
		return nil, errors.Errorf("Unexpected literal type %v", n.Typex)
	}

	return &BoundLiteral{ExprType: exprType, Value: n.Value}, nil
}
func (b *propertyBinder) bindOutput(n *ast.Output) (BoundExpr, error) {
	exprs, err := b.bindExprs(n.Exprs)
	if err != nil {
		return nil, err
	}

	// Project a single-element output to the element itself.
	if len(exprs) == 1 {
		return exprs[0], nil
	}

	return &BoundOutput{HILNode: n, Exprs: exprs}, nil
}

func (b *propertyBinder) bindVariableAccess(n *ast.VariableAccess) (BoundExpr, error) {
	tfVar, err := config.NewInterpolatedVariable(n.Name)
	if err != nil {
		return nil, err
	}

	elements, sch, exprType, ilNode := []string(nil), Schemas{}, TypeUnknown, Node(nil)
	switch v := tfVar.(type) {
	case *config.CountVariable:
		// "count."
		if v.Type != config.CountValueIndex {
			return nil, errors.Errorf("unsupported count variable %s", v.FullKey())
		}

		if !b.hasCountIndex {
			return nil, errors.Errorf("no count index in scope")
		}

		exprType = TypeNumber
	case *config.LocalVariable:
		// "local."
		return nil, errors.New("NYI: local variables")
	case *config.ModuleVariable:
		// "module."
		return nil, errors.New("NYI: module variables")
	case *config.PathVariable:
		// "path."
		return nil, errors.New("NYI: path variables")
	case *config.ResourceVariable:
		// default

		// look up the resource.
		r, ok := b.builder.resources[v.ResourceId()]
		if !ok {
			return nil, errors.Errorf("unknown resource %v", v.ResourceId())
		}
		ilNode = r

		// ensure that the resource has a provider.
		if err := b.builder.ensureProvider(r); err != nil {
			return nil, err
		}

		// fetch the resource's schema info
		if r.Provider.Info != nil {
			resInfo := r.Provider.Info.Resources[v.Type]
			sch.TFRes = r.Provider.Info.P.ResourcesMap[v.Type]
			sch.Pulumi = &tfbridge.SchemaInfo{Fields: resInfo.Fields}
		}

		// name{.property}+
		elements = strings.Split(v.Field, ".")
		elemSch := sch
		for _, e := range elements {
			elemSch = elemSch.PropertySchemas(e)
		}

		// Handle multi-references (splats and indexes)
		exprType = elemSch.Type()
		if v.Multi && v.Index == -1 {
			exprType = exprType.ListOf()
		}
	case *config.SelfVariable:
		// "self."
		return nil, errors.New("NYI: self variables")
	case *config.SimpleVariable:
		// "[^.]\+"
		return nil, errors.New("NYI: simple variables")
	case *config.TerraformVariable:
		// "terraform."
		return nil, errors.New("NYI: terraform variables")
	case *config.UserVariable:
		// "var."
		if v.Elem != "" {
			return nil, errors.New("NYI: user variable elements")
		}

		// look up the variable.
		vn, ok := b.builder.variables[v.Name]
		if !ok {
			return nil, errors.Errorf("unknown variable %s", v.Name)
		}
		ilNode = vn

		// If the variable does not have a default, its type is string. If it does have a default, its type is string
		// iff the default's type is also string. Note that we don't try all that hard here.
		exprType = TypeString
		if vn.DefaultValue != nil && vn.DefaultValue.Type() != TypeString {
			exprType = TypeUnknown
		}
	default:
		return nil, errors.Errorf("unexpected variable type %T", v)
	}

	return &BoundVariableAccess{
		HILNode:  n,
		Elements: elements,
		Schemas:  sch,
		ExprType: exprType,
		TFVar:    tfVar,
		ILNode:   ilNode,
	}, nil
}

func (b *propertyBinder) bindExprs(ns []ast.Node) ([]BoundExpr, error) {
	boundExprs := make([]BoundExpr, len(ns))
	for i, n := range ns {
		bn, err := b.bindExpr(n)
		if err != nil {
			return nil, err
		}
		boundExprs[i] = bn
	}
	return boundExprs, nil
}

func (b *propertyBinder) bindExpr(n ast.Node) (BoundExpr, error) {
	switch n := n.(type) {
	case *ast.Arithmetic:
		return b.bindArithmetic(n)
	case *ast.Call:
		return b.bindCall(n)
	case *ast.Conditional:
		return b.bindConditional(n)
	case *ast.Index:
		return b.bindIndex(n)
	case *ast.LiteralNode:
		return b.bindLiteral(n)
	case *ast.Output:
		return b.bindOutput(n)
	case *ast.VariableAccess:
		return b.bindVariableAccess(n)
	default:
		return nil, errors.Errorf("unexpected HIL node type %T", n)
	}
}

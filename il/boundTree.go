package il

import (
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"
)

type Type uint32

const (
	TypeInvalid Type = 0
	TypeBool    Type = 1
	TypeString  Type = 1 << 1
	TypeNumber  Type = 1 << 2
	TypeMap     Type = 1 << 3
	TypeUnknown Type = 1 << 4

	TypeList   Type = 1 << 5
	TypeOutput Type = 1 << 6

	elementTypeMask Type = TypeBool | TypeString | TypeNumber | TypeMap | TypeUnknown
)

func (t Type) IsList() bool {
	return t&TypeList != 0
}

func (t Type) ListOf() Type {
	return t | TypeList
}

func (t Type) IsOutput() bool {
	return t&TypeOutput != 0
}

func (t Type) OutputOf() Type {
	return t | TypeOutput
}

func (t Type) ElementType() Type {
	return t & elementTypeMask
}

type BoundNode interface {
	Type() Type

	isNode()
}

type BoundExpr interface {
	BoundNode

	isExpr()
}

type BoundArithmetic struct {
	HILNode *ast.Arithmetic

	Exprs []BoundExpr
}

func (n *BoundArithmetic) Type() Type {
	return TypeNumber
}

func (n *BoundArithmetic) isNode() {}
func (n *BoundArithmetic) isExpr() {}

type BoundCall struct {
	HILNode  *ast.Call
	ExprType Type

	Args []BoundExpr
}

func (n *BoundCall) Type() Type {
	return n.ExprType
}

func (n *BoundCall) isNode() {}
func (n *BoundCall) isExpr() {}

type BoundConditional struct {
	HILNode  *ast.Conditional
	ExprType Type

	CondExpr  BoundExpr
	TrueExpr  BoundExpr
	FalseExpr BoundExpr
}

func (n *BoundConditional) Type() Type {
	return n.ExprType
}

func (n *BoundConditional) isNode() {}
func (n *BoundConditional) isExpr() {}

type BoundIndex struct {
	HILNode  *ast.Index
	ExprType Type

	TargetExpr BoundExpr
	KeyExpr    BoundExpr
}

func (n *BoundIndex) Type() Type {
	return n.ExprType
}

func (n *BoundIndex) isNode() {}
func (n *BoundIndex) isExpr() {}

type BoundLiteral struct {
	ExprType Type
	Value    interface{}
}

func (n *BoundLiteral) Type() Type {
	return n.ExprType
}

func (n *BoundLiteral) isNode() {}
func (n *BoundLiteral) isExpr() {}

type BoundOutput struct {
	HILNode *ast.Output

	Exprs []BoundExpr
}

func (n *BoundOutput) Type() Type {
	return TypeString
}

func (n *BoundOutput) isNode() {}
func (n *BoundOutput) isExpr() {}

type BoundVariableAccess struct {
	HILNode  *ast.VariableAccess
	Elements []string
	Schemas  Schemas
	ExprType Type

	TFVar  config.InterpolatedVariable
	ILNode Node
}

func (n *BoundVariableAccess) Type() Type {
	return n.ExprType
}

func (n *BoundVariableAccess) isNode() {}
func (n *BoundVariableAccess) isExpr() {}

type BoundListProperty struct {
	Schemas  Schemas
	Elements []BoundNode
}

func (n *BoundListProperty) Type() Type {
	return n.Schemas.ElemSchemas().Type().ListOf()
}

func (n *BoundListProperty) isNode() {}

type BoundMapProperty struct {
	Schemas  Schemas
	Elements map[string]BoundNode
}

func (n *BoundMapProperty) Type() Type {
	return TypeMap
}

func (n *BoundMapProperty) isNode() {}

type BoundNodeVisitor func(n BoundNode) (BoundNode, error)

func IdentityVisitor(n BoundNode) (BoundNode, error) {
	return n, nil
}

func visitBoundArithmetic(n *BoundArithmetic, pre, post BoundNodeVisitor) (BoundNode, error) {
	exprs, err := visitBoundExprs(n.Exprs, pre, post)
	if err != nil {
		return nil, err
	}
	if len(exprs) == 0 {
		return nil, nil
	}
	n.Exprs = exprs
	return post(n)
}

func visitBoundCall(n *BoundCall, pre, post BoundNodeVisitor) (BoundNode, error) {
	exprs, err := visitBoundExprs(n.Args, pre, post)
	if err != nil {
		return nil, err
	}
	n.Args = exprs
	return post(n)
}

func visitBoundConditional(n *BoundConditional, pre, post BoundNodeVisitor) (BoundNode, error) {
	condExpr, err := VisitBoundExpr(n.CondExpr, pre, post)
	if err != nil {
		return nil, err
	}
	trueExpr, err := VisitBoundExpr(n.TrueExpr, pre, post)
	if err != nil {
		return nil, err
	}
	falseExpr, err := VisitBoundExpr(n.FalseExpr, pre, post)
	if err != nil {
		return nil, err
	}
	n.CondExpr, n.TrueExpr, n.FalseExpr = condExpr, trueExpr, falseExpr
	return post(n)
}

func visitBoundIndex(n *BoundIndex, pre, post BoundNodeVisitor) (BoundNode, error) {
	targetExpr, err := VisitBoundExpr(n.TargetExpr, pre, post)
	if err != nil {
		return nil, err
	}
	keyExpr, err := VisitBoundExpr(n.KeyExpr, pre, post)
	if err != nil {
		return nil, err
	}
	n.TargetExpr, n.KeyExpr = targetExpr, keyExpr
	return post(n)
}

func visitBoundListProperty(n *BoundListProperty, pre, post BoundNodeVisitor) (BoundNode, error) {
	exprs, err := visitBoundNodes(n.Elements, pre, post)
	if err != nil {
		return nil, err
	}
	if len(exprs) == 0 {
		return nil, nil
	}
	n.Elements = exprs
	return post(n)
}

func visitBoundMapProperty(n *BoundMapProperty, pre, post BoundNodeVisitor) (BoundNode, error) {
	for k, e := range n.Elements {
		ee, err := VisitBoundNode(e, pre, post)
		if err != nil {
			return nil, err
		}
		if ee == nil {
			delete(n.Elements, k)
		} else {
			n.Elements[k] = ee
		}
	}
	return post(n)
}

func visitBoundOutput(n *BoundOutput, pre, post BoundNodeVisitor) (BoundNode, error) {
	exprs, err := visitBoundExprs(n.Exprs, pre, post)
	if err != nil {
		return nil, err
	}
	if len(exprs) == 0 {
		return nil, nil
	}
	n.Exprs = exprs
	return post(n)
}

func visitBoundExprs(ns []BoundExpr, pre, post BoundNodeVisitor) ([]BoundExpr, error) {
	nils := 0
	for i, e := range ns {
		ee, err := VisitBoundExpr(e, pre, post)
		if err != nil {
			return nil, err
		}
		if ee == nil {
			nils++
		}
		ns[i] = ee
	}
	if nils == 0 {
		return ns, nil
	} else if nils == len(ns) {
		return []BoundExpr{}, nil
	}

	nns := make([]BoundExpr, 0, len(ns)-nils)
	for _, e := range ns {
		if e != nil {
			nns = append(nns, e)
		}
	}
	return nns, nil
}

func visitBoundNodes(ns []BoundNode, pre, post BoundNodeVisitor) ([]BoundNode, error) {
	nils := 0
	for i, e := range ns {
		ee, err := VisitBoundNode(e, pre, post)
		if err != nil {
			return nil, err
		}
		if ee == nil {
			nils++
		}
		ns[i] = ee
	}
	if nils == 0 {
		return ns, nil
	} else if nils == len(ns) {
		return []BoundNode{}, nil
	}

	nns := make([]BoundNode, 0, len(ns)-nils)
	for _, e := range ns {
		if e != nil {
			nns = append(nns, e)
		}
	}
	return nns, nil
}

func VisitBoundNode(n BoundNode, pre, post BoundNodeVisitor) (BoundNode, error) {
	nn, err := pre(n)
	if err != nil {
		return nil, err
	}
	n = nn

	switch n := n.(type) {
	case *BoundArithmetic:
		return visitBoundArithmetic(n, pre, post)
	case *BoundCall:
		return visitBoundCall(n, pre, post)
	case *BoundConditional:
		return visitBoundConditional(n, pre, post)
	case *BoundIndex:
		return visitBoundIndex(n, pre, post)
	case *BoundListProperty:
		return visitBoundListProperty(n, pre, post)
	case *BoundLiteral:
		return post(n)
	case *BoundMapProperty:
		return visitBoundMapProperty(n, pre, post)
	case *BoundOutput:
		return visitBoundOutput(n, pre, post)
	case *BoundVariableAccess:
		return post(n)
	default:
		contract.Failf("unexpected node type in visitBoundExpr: %T", n)
		return nil, errors.Errorf("unexpected node type in visitBoundExpr: %T", n)
	}
}

func VisitBoundExpr(n BoundExpr, pre, post BoundNodeVisitor) (BoundExpr, error) {
	nn, err := VisitBoundNode(n, pre, post)
	if err == nil || nn == nil {
		return nil, err
	}
	return nn.(BoundExpr), nil
}

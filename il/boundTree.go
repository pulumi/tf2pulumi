package il

import (
	"fmt"
	"io"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"
)

// TODO: real, actual output types :groan:

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

func (t Type) String() string {
	s := "invalid"
	switch t.ElementType() {
	case TypeBool:
		s = "bool"
	case TypeString:
		s = "string"
	case TypeNumber:
		s = "number"
	case TypeMap:
		s = "map"
	case TypeUnknown:
		s = "unknown"
	default:
		contract.Failf("unknown element type")
	}
	if t.IsList() {
		s = fmt.Sprintf("list<%s>", s)
	}
	if t.IsOutput() {
		s = fmt.Sprintf("output<%s>", s)
	}
	return s

}

type dumper struct {
	w      io.Writer
	indent string
}

func (d *dumper) indented(f func()) {
	indent := d.indent
	d.indent += "    "
	defer func() { d.indent = indent }()
	f()
}

func (d *dumper) dump(vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			fmt.Fprint(d.w, v)
		case BoundNode:
			v.dump(d)
		default:
			panic("unexpected type in dump")
		}
	}
}

type BoundNode interface {
	Type() Type

	dump(d *dumper)
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

func (n *BoundArithmetic) dump(d *dumper) {
	d.dump("(", fmt.Sprintf("%v %v", n.Type(), n.HILNode.Op))
	d.indented(func() {
		for _, e := range n.Exprs {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
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

func (n *BoundCall) dump(d *dumper) {
	d.dump("(call ", fmt.Sprintf("%v %s", n.Type(), n.HILNode.Func))
	d.indented(func() {
		for _, e := range n.Args {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
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

func (n *BoundConditional) dump(d *dumper) {
	d.dump("(cond ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		d.dump("\n", d.indent, n.CondExpr)
		d.dump("\n", d.indent, n.TrueExpr)
		d.dump("\n", d.indent, n.FalseExpr)
	})
	d.dump("\n", d.indent, ")")
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

func (n *BoundIndex) dump(d *dumper) {
	d.dump("(index ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		d.dump("\n", d.indent, n.TargetExpr)
		d.dump("\n", d.indent, n.KeyExpr)
	})
	d.dump("\n", d.indent, ")")
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

func (n *BoundLiteral) dump(d *dumper) {
	switch n.ExprType {
	case TypeString:
		d.dump(fmt.Sprintf("%q", n.Value))
	default:
		d.dump(fmt.Sprintf("%v", n.Value))
	}
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

func (n *BoundOutput) dump(d *dumper) {
	d.dump("(output ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		for _, e := range n.Exprs {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
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

func (n *BoundVariableAccess) dump(d *dumper) {
	d.dump(fmt.Sprintf("(%s %s %T)", n.HILNode.Name, n.Type(), n.TFVar))
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

func (n *BoundListProperty) dump(d *dumper) {
	d.dump("(list ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		for _, e := range n.Elements {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundListProperty) isNode() {}

type BoundMapProperty struct {
	Schemas  Schemas
	Elements map[string]BoundNode
}

func (n *BoundMapProperty) Type() Type {
	return TypeMap
}

func (n *BoundMapProperty) dump(d *dumper) {
	d.dump("(map ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		for k, e := range n.Elements {
			d.dump("\n", d.indent, k, ": ", e)
		}
	})
	d.dump("\n", d.indent, ")")
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
	if err != nil || nn == nil {
		return nil, err
	}
	return nn.(BoundExpr), nil
}

func DumpBoundNode(w io.Writer, e BoundNode) {
	e.dump(&dumper{w: w})
	fmt.Fprint(w, "\n")
}

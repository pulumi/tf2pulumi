package nodejs

import (
	"github.com/hashicorp/hil/ast"

	"github.com/pgavlin/firewalker/il"
)

func makeCoercion(n il.BoundNode, toType il.Type) il.BoundNode {
	// TODO: we really need dynamic coercions for the negative case.
	from, to := n.Type().ElementType(), toType.ElementType()

	e, ok := n.(il.BoundExpr)
	if !ok || from == to {
		return n
	}

	switch from {
	case il.TypeBool, il.TypeNumber:
		if to != il.TypeString {
			return n
		}
	case il.TypeString:
		if to != il.TypeBool && to != il.TypeNumber {
			return n
		}
	default:
		return n
	}

	return &il.BoundCall{
		HILNode: &ast.Call{Func: "__coerce"},
		ExprType: toType,
		Args: []il.BoundExpr{e},
	}
}

func addCoercions(prop il.BoundNode) (il.BoundNode, error) {
	rewriter := func(n il.BoundNode) (il.BoundNode, error) {
		switch n := n.(type) {
		case *il.BoundListProperty:
			elemType := n.Schemas.ElemSchemas().Type()
			for i := range n.Elements {
				n.Elements[i] = makeCoercion(n.Elements[i], elemType)
			}
		case *il.BoundMapProperty:
			for k := range n.Elements {
				n.Elements[k] = makeCoercion(n.Elements[k], n.Schemas.PropertySchemas(k).Type())
			}
		}
		return n, nil
	}

	return il.VisitBoundNode(prop, il.IdentityVisitor, rewriter)
}

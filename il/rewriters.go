package il

import (
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"
)

type applyRewriter struct {
	root      BoundExpr
	applyArgs []BoundExpr
}

func (r *applyRewriter) rewriteBoundVariableAccess(n *BoundVariableAccess) (BoundExpr, error) {
	if !n.Type().IsOutput() {
		return n, nil
	}

	idx := len(r.applyArgs)
	r.applyArgs = append(r.applyArgs, n)

	return &BoundCall{
		HILNode:  &ast.Call{Func: "__applyArg"},
		ExprType: n.Type().ElementType(),
		Args:     []BoundExpr{&BoundLiteral{ExprType: TypeNumber, Value: idx}},
	}, nil
}

func (r *applyRewriter) rewriteRoot(n BoundExpr) (BoundNode, error) {
	contract.Require(n == r.root, "n")

	r.root = nil
	if len(r.applyArgs) == 0 {
		return n, nil
	}

	r.applyArgs = append(r.applyArgs, n)

	return &BoundCall{
		HILNode:  &ast.Call{Func: "__apply"},
		ExprType: TypeUnknown.OutputOf(),
		Args:     r.applyArgs,
	}, nil
}

func (r *applyRewriter) rewriteNode(n BoundNode) (BoundNode, error) {
	if e, ok := n.(BoundExpr); ok {
		v, isVar := e.(*BoundVariableAccess)
		if e == r.root {
			if isVar {
				rv, ok := v.TFVar.(*config.ResourceVariable)
				if ok {
					// If we're accessing a field of a data source or a nested field of a resource, we need to perform an
					// apply. As such, we'll synthesize an output here.
					if rv.Mode == config.DataResourceMode && len(v.Elements) > 0 || len(v.Elements) > 1 {
						ee, err := r.rewriteBoundVariableAccess(v)
						if err != nil {
							return nil, err
						}
						e, r.root = ee, ee
					}
				}
			}
			return r.rewriteRoot(e)
		}
		if isVar {
			return r.rewriteBoundVariableAccess(v)
		}
	}
	return n, nil
}

func (r *applyRewriter) enterNode(n BoundNode) (BoundNode, error) {
	e, ok := n.(BoundExpr)
	if ok && r.root == nil {
		r.root, r.applyArgs = e, nil
	}
	return n, nil
}

func RewriteApplies(n BoundNode) (BoundNode, error) {
	rewriter := &applyRewriter{}
	return VisitBoundNode(n, rewriter.enterNode, rewriter.rewriteNode)
}

func RewriteAssets(n BoundNode) (BoundNode, error) {
	rewriter := func(n BoundNode) (BoundNode, error) {
		m, ok := n.(*BoundMapProperty)
		if !ok {
			return n, nil
		}

		for k, v := range m.Elements {
			e, ok := v.(BoundExpr)
			if !ok {
				continue
			}

			elemSch := m.Schemas.PropertySchemas(k)
			if elemSch.Pulumi == nil || elemSch.Pulumi.Asset == nil {
				continue
			}

			asset := elemSch.Pulumi.Asset

			builtin := "__asset"
			if asset.Kind == tfbridge.FileArchive || asset.Kind == tfbridge.BytesArchive {
				builtin = "__archive"
			}

			m.Elements[k] = &BoundCall{
				HILNode:  &ast.Call{Func: builtin},
				ExprType: TypeUnknown,
				Args:     []BoundExpr{e},
			}

			if asset.HashField != "" {
				delete(m.Elements, asset.HashField)
			}
		}

		return m, nil
	}

	return VisitBoundNode(n, IdentityVisitor, rewriter)
}

package il

import (
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type applyRewriter struct {
	root BoundExpr
	applyArgs []BoundExpr
}

func (r *applyRewriter) rewriteBoundVariableAccess(n *BoundVariableAccess) (BoundNode, error) {
	contract.Assert(r.root != n)

	if !n.Type().IsOutput() {
		return n, nil
	}

	idx := len(r.applyArgs)
	r.applyArgs = append(r.applyArgs, n)

	return &BoundCall{
		HILNode: &ast.Call{Func: "__applyArg"},
		ExprType: n.Type().ElementType(),
		Args: []BoundExpr{&BoundLiteral{ExprType: TypeNumber, Value: idx}},
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
		HILNode: &ast.Call{Func: "__apply"},
		ExprType: TypeUnknown.OutputOf(),
		Args: r.applyArgs,
	}, nil
}

func (r *applyRewriter) rewriteNode(n BoundNode) (BoundNode, error) {
	if e, ok := n.(BoundExpr); ok {
		if e == r.root {
			return r.rewriteRoot(e)
		}
		if v, ok := e.(*BoundVariableAccess); ok {
			return r.rewriteBoundVariableAccess(v)
		}
	}
	return n, nil
}

func (r *applyRewriter) enterNode(n BoundNode) (BoundNode, error) {
	e, ok := n.(BoundExpr)
	if !ok || r.root != nil {
		return n, nil
	}

	r.root, r.applyArgs = e, nil
	if v, ok := n.(*BoundVariableAccess); ok {
		rv, ok := v.TFVar.(*config.ResourceVariable)
		if ok {
			// If we're accessing a field of a data source or a nested field of a resource, we need to perform an
			// apply. As such, we'll synthesize an output here.
			if rv.Mode == config.DataResourceMode && len(v.Elements) > 0 || len(v.Elements) > 1 {
				r.root = &BoundOutput{Exprs:[]BoundExpr{v}}
				return r.root, nil
			}
		}
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
				HILNode: &ast.Call{Func: builtin},
				ExprType: TypeUnknown,
				Args: []BoundExpr{e},
			}

			if asset.HashField != "" {
				delete(m.Elements, asset.HashField)
			}
		}

		return m, nil
	}

	return VisitBoundNode(n, IdentityVisitor, rewriter)
}


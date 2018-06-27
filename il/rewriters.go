package il

import (
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type applyRewriter struct {
	output *BoundOutput
	applyArgs []BoundExpr
}

func (r *applyRewriter) rewriteBoundVariableAccess(n *BoundVariableAccess) (BoundNode, error) {
	if r.output == nil {
		return n, nil
	}

	_, ok := n.TFVar.(*config.ResourceVariable)
	if !ok {
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

func (r *applyRewriter) rewriteBoundOutput(n *BoundOutput) (BoundNode, error) {
	r.output = nil

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
	switch n := n.(type) {
	case *BoundVariableAccess:
		return r.rewriteBoundVariableAccess(n)
	case *BoundOutput:
		return r.rewriteBoundOutput(n)
	default:
		return n, nil
	}
}

func (r *applyRewriter) enterNode(n BoundNode) (BoundNode, error) {
	if o, ok := n.(*BoundOutput); ok {
		r.output, r.applyArgs = o, nil
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


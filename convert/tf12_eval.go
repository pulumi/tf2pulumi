package convert

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/terraform/addrs"
	"github.com/hashicorp/terraform/lang"
	"github.com/hashicorp/terraform/tfdiags"
	"github.com/pulumi/pulumi/pkg/v2/codegen"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2/model"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

type conditionalAnalyzer struct {
	analyzedNodes codegen.Set
	booleanValues codegen.Set
}

func newConditionalAnalyzer() *conditionalAnalyzer {
	return &conditionalAnalyzer{
		analyzedNodes: codegen.Set{},
		booleanValues: codegen.Set{},
	}
}

func (analyzer *conditionalAnalyzer) isConditionalValue(n model.Expression) bool {
	_, diags := model.VisitExpression(n, model.IdentityVisitor, analyzer.postVisit)
	contract.Assert(len(diags) == 0)
	return analyzer.booleanValues.Has(n)
}

func (analyzer *conditionalAnalyzer) postVisit(n model.Expression) (model.Expression, hcl.Diagnostics) {
	if model.BoolType.ConversionFrom(n.Type()) == model.SafeConversion {
		analyzer.booleanValues.Add(n)
		return n, nil
	}

	switch n := n.(type) {
	case *model.ConditionalExpression:
		if analyzer.booleanValues.Has(n.TrueResult) && analyzer.booleanValues.Has(n.FalseResult) {
			analyzer.booleanValues.Add(n)
		}
	case *model.LiteralValueExpression:
		if _, err := convert.Convert(n.Value, cty.Bool); err != nil {
			analyzer.booleanValues.Add(n)
		}
	case *model.ScopeTraversalExpression:
		if local, ok := n.Parts[0].(*local); ok {
			if !analyzer.analyzedNodes.Has(local) {
				if analyzer.isConditionalValue(local.attribute.Value) {
					analyzer.booleanValues.Add(local)
				}
				analyzer.analyzedNodes.Add(local)
			}
			if analyzer.booleanValues.Has(local) {
				analyzer.booleanValues.Add(n)
			}
		}
	}

	return n, nil
}

func (b *tf12binder) StaticValidateReferences(refs []*addrs.Reference, self addrs.Referenceable) tfdiags.Diagnostics {
	return nil
}

func (b *tf12binder) GetCountAttr(addr addrs.CountAttr, r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return cty.UnknownVal(cty.Number), nil
}

func (b *tf12binder) GetForEachAttr(addr addrs.ForEachAttr, r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetResource(addr addrs.Resource, r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetLocalValue(addr addrs.LocalValue, r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	scope, _ := b.root.BindReference("local")
	if l, ok := scope.(*model.Scope).BindReference(addr.Name); ok {
		var diagnostics tfdiags.Diagnostics

		expr := model.HCLExpression(l.(*local).attribute.Value)

		refs, refsDiags := lang.ReferencesInExpr(expr)
		diagnostics.Append(refsDiags)
		if diagnostics.HasErrors() {
			return cty.UnknownVal(cty.DynamicPseudoType), diagnostics
		}

		ctx, ctxDiags := (&lang.Scope{Data: b}).EvalContext(refs)
		diagnostics.Append(ctxDiags)
		if diagnostics.HasErrors() {
			return cty.UnknownVal(cty.DynamicPseudoType), diagnostics
		}

		val, valDiags := expr.Value(ctx)
		diagnostics.Append(valDiags)
		return val, diagnostics
	}
	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetModuleInstance(addr addrs.ModuleCallInstance,
	r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {

	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetModuleInstanceOutput(addr addrs.ModuleCallOutput,
	r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {

	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetPathAttr(addr addrs.PathAttr, r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetTerraformAttr(addr addrs.TerraformAttr,
	r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {

	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

func (b *tf12binder) GetInputVariable(addr addrs.InputVariable,
	r tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {

	return cty.UnknownVal(cty.DynamicPseudoType), nil
}

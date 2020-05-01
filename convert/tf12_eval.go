package convert

import (
	"github.com/hashicorp/hcl/v2"
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
		strVal, err := convert.Convert(n.Value, cty.String)
		if err == nil {
			if _, err = convert.Convert(strVal, cty.Bool); err == nil {
				analyzer.booleanValues.Add(n)
			}
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

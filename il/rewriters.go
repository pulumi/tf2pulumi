// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package il

import (
	"sort"

	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/internal/config"
)

// The applyRewriter is responsible for transforming expressions involving Pulumi output properties into a call to the
// __apply intrinsic and replacing the output properties with appropriate calls to the __applyArg intrinsic.
type applyRewriter struct {
	root      BoundExpr
	applyArgs []*BoundVariableAccess
}

// rewriteBoundVariableAccess replaces a single access to an ouptut-typed BoundVariableAccess with a call to the
// __applyArg intrinsic.
func (r *applyRewriter) rewriteBoundVariableAccess(n *BoundVariableAccess) (BoundExpr, error) {
	// If the access is not output-typed, return the node as-is.
	if !n.Type().IsOutput() {
		return n, nil
	}

	// Otherwise, append the access to the list of apply arguments and return an appropriate call to __applyArg.
	//
	// TODO: deduplicate multiple accesses to the same variable and field.
	idx := len(r.applyArgs)
	r.applyArgs = append(r.applyArgs, n)

	return NewApplyArgCall(idx, n.Type().ElementType()), nil
}

// rewriteRoot replaces the root node in a bound expression with a call to the __apply intrinsic if necessary.
func (r *applyRewriter) rewriteRoot(n BoundExpr) (BoundNode, error) {
	contract.Require(n == r.root, "n")

	// Clear the root context so that future calls to enterNode recognize new expression roots.
	r.root = nil
	if len(r.applyArgs) == 0 {
		return n, nil
	}

	return NewApplyCall(r.applyArgs, n), nil
}

// rewriteNode performs the apply rewrite on a single node, delegating to type-specific functions as necessary.
func (r *applyRewriter) rewriteNode(n BoundNode) (BoundNode, error) {
	if e, ok := n.(BoundExpr); ok {
		v, isVar := e.(*BoundVariableAccess)
		if e == r.root {
			if isVar {
				rv, ok := v.TFVar.(*config.ResourceVariable)
				if ok {
					// If we're accessing a field of a data source or a nested field of a resource, we need to
					// perform an apply. As such, we'll synthesize an output here.
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

// enterNode is a pre-order visitor that is used to find roots for bound expression trees. This approach is intended to
// allow consumers of the apply rewrite to call RewriteApplies on a list or map property that may contain multiple
// independent bound expressions rather than requiring that they find and rewrite these expressions individually.
func (r *applyRewriter) enterNode(n BoundNode) (BoundNode, error) {
	e, ok := n.(BoundExpr)
	if ok && r.root == nil {
		r.root, r.applyArgs = e, nil
	}
	return n, nil
}

// RewriteApplies transforms all bound expression trees in the given BoundNode that reference output-typed properties
// into appropriate calls to the __apply and __applyArg intrinsic. Given an expression tree, the rewrite proceeds as
// follows:
// - let the list of outputs be an empty list
// - for each node in post-order:
//     - if the node is the root of the expression tree:
//         - if the node is a variable access:
//             - if the access has an output-typed element on its path, replace the variable access with a call to the
//               __applyArg intrinsic and append the access to the list of outputs.
//             - otherwise, the access does not need to be transformed; return it as-is.
//         - if the list of outputs is empty, the root does not need to be transformed; return it as-is.
//         - otherwise, replace the root with a call to the __apply intrinstic. The first n arguments to this call are
//           the elementss of the list of outputs. The final argument is the original root node.
//     - otherwise, if the root is an output-typed variable access, replace the variable access with a call to the
//       __applyArg instrinsic and append the access to the list of outputs.
//
// As an example, this transforms the following expression:
//     (output string
//         "#!/bin/bash -xe\n\nCA_CERTIFICATE_DIRECTORY=/etc/kubernetes/pki\necho \""
//         (aws_eks_cluster.demo.certificate_authority.0.data output<unknown> *config.ResourceVariable)
//         "\" | base64 -d >  $CA_CERTIFICATE_FILE_PATH\nsed -i s,MASTER_ENDPOINT,"
//         (aws_eks_cluster.demo.endpoint output<string> *config.ResourceVariable)
//         ",g /var/lib/kubelet/kubeconfig\nsed -i s,CLUSTER_NAME,"
//         (var.cluster-name string *config.UserVariable)
//         ",g /var/lib/kubelet/kubeconfig\nsed -i s,REGION,"
//         (data.aws_region.current.name output<string> *config.ResourceVariable)
//         ",g /etc/systemd/system/kubelet.servicesed -i s,MASTER_ENDPOINT,"
//         (aws_eks_cluster.demo.endpoint output<string> *config.ResourceVariable)
//         ",g /etc/systemd/system/kubelet.service"
//     )
//
// into this expression:
//     (call output<unknown> __apply
//         (aws_eks_cluster.demo.certificate_authority.0.data output<unknown> *config.ResourceVariable)
//         (aws_eks_cluster.demo.endpoint output<string> *config.ResourceVariable)
//         (data.aws_region.current.name output<string> *config.ResourceVariable)
//         (aws_eks_cluster.demo.endpoint output<string> *config.ResourceVariable)
//         (output string
//             "#!/bin/bash -xe\n\nCA_CERTIFICATE_DIRECTORY=/etc/kubernetes/pki\necho \""
//             (call unknown __applyArg
//                 0
//             )
//             "\" | base64 -d >  $CA_CERTIFICATE_FILE_PATH\nsed -i s,MASTER_ENDPOINT,"
//             (call string __applyArg
//                 1
//             )
//             ",g /var/lib/kubelet/kubeconfig\nsed -i s,CLUSTER_NAME,"
//             (var.cluster-name string *config.UserVariable)
//             ",g /var/lib/kubelet/kubeconfig\nsed -i s,REGION,"
//             (call string __applyArg
//                 2
//             )
//             ",g /etc/systemd/system/kubelet.servicesed -i s,MASTER_ENDPOINT,"
//             (call string __applyArg
//                 3
//             )
//             ",g /etc/systemd/system/kubelet.service"
//         )
//     )
//
// This form is amenable to code generation for targets that require that outputs are resolved before their values are
// accessible (e.g. Pulumi's JS/TS libraries).
func RewriteApplies(n BoundNode) (BoundNode, error) {
	rewriter := &applyRewriter{}
	return VisitBoundNode(n, rewriter.enterNode, rewriter.rewriteNode)
}

// RewriteAssets transforms all arguments to Terraform properties that are projected as Pulumi assets or archives into
// calls to the appropriate __asset or __archive intrinsic.
func RewriteAssets(n BoundNode) (BoundNode, error) {
	rewriter := func(n BoundNode) (BoundNode, error) {
		m, ok := n.(*BoundMapProperty)
		if !ok {
			return n, nil
		}

		// Sort keys to ensure we visit in a deterministic order.
		var keys []string
		for k := range m.Elements {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			e, ok := m.Elements[k].(BoundExpr)
			if !ok {
				continue
			}

			elemSch := m.Schemas.PropertySchemas(k)
			if elemSch.Pulumi == nil || elemSch.Pulumi.Asset == nil {
				continue
			}
			asset := elemSch.Pulumi.Asset

			// If the argument to this parameter is an archive resource, strip the field off of the variable access and
			// pass the variable directly.
			isArchiveResource := false
			if v, ok := e.(*BoundVariableAccess); ok {
				if r, ok := v.ILNode.(*ResourceNode); ok && r.Provider.Name == "archive" {
					v.Elements = nil
					isArchiveResource = true
				}
			}

			if !isArchiveResource {
				var call BoundExpr
				if asset.Kind == tfbridge.FileArchive || asset.Kind == tfbridge.BytesArchive {
					call = NewArchiveCall(e)
				} else {
					call = NewAssetCall(e)
				}
				m.Elements[k] = call
			}

			if asset.HashField != "" {
				delete(m.Elements, asset.HashField)
			}
		}

		return m, nil
	}

	return VisitBoundNode(n, IdentityVisitor, rewriter)
}

// FilterProperties removes any properties at the root of the given resource for which the given filter function
// returns false.
func FilterProperties(r *ResourceNode, filter func(key string, property BoundNode) bool) {
	for key, prop := range r.Properties.Elements {
		if !filter(key, prop) {
			delete(r.Properties.Elements, key)
		}
	}
}

// MarkPromptDataSources finds all data sources with no Output-typed inputs, marks these data sources as prompt,
// and retypes all variable accesses rooted in these data sources appropriately.
func MarkPromptDataSources(g *Graph) map[*ResourceNode]bool {
	// Mark any datasources with no output-typed inputs as prompt. Do this until we reach a fixed point.
	promptDataSources := make(map[*ResourceNode]bool)

	for {
		changed := false

		// First, check all data sources for output-typed inputs.
		for _, r := range g.Resources {
			if r.IsDataSource {
				containsOutputs := false
				_, err := VisitBoundNode(r.Properties, IdentityVisitor, func(n BoundNode) (BoundNode, error) {
					containsOutputs = containsOutputs || n.Type().IsOutput()
					return n, nil
				})
				contract.Assert(err == nil)

				if !containsOutputs && !promptDataSources[r] {
					promptDataSources[r] = true
					changed = true
				}
			}
		}

		// If nothing changed, we are done.
		if !changed {
			return promptDataSources
		}

		// Otherwise, retype any data source accesses as appropriate.
		err := VisitAllProperties(g, IdentityVisitor, func(n BoundNode) (BoundNode, error) {
			if n, ok := n.(*BoundVariableAccess); ok {
				if r, ok := n.ILNode.(*ResourceNode); ok {
					if promptDataSources[r] {
						n.ExprType = n.ExprType & ^TypeOutput
					}
				}
			}
			return n, nil
		})
		contract.Assert(err == nil)
	}

}

// isBooleanValue rerturns true if the given expression produces a value that can be considered to be true or false.
func isBooleanValue(expr BoundExpr) bool {
	// Any expression that is boolean-typed is by definition a boolean value.
	if expr.Type() == TypeBool {
		return true
	}

	switch expr := expr.(type) {
	case *BoundConditional:
		// The result of a conditional is a boolean value if both legs of the expression are boolean values.
		return isBooleanValue(expr.TrueExpr) && isBooleanValue(expr.FalseExpr)
	case *BoundLiteral:
		// A literal is a boolean value if it can be successfully coerced to a boolean value.
		_, ok := coerceLiteral(expr, expr.Type(), TypeBool)
		return ok
	case *BoundVariableAccess:
		// A variable access is a boolean value if it is a local whose value is a boolean value.
		local, ok := expr.ILNode.(*LocalNode)
		if !ok {
			return false
		}
		if valueExpr, ok := local.Value.(BoundExpr); ok {
			return isBooleanValue(valueExpr)
		}
		return false
	default:
		// All other expressions--arithmetic expressions, calls, index expressions, and output expressions--are not
		// considered as producing boolean values. Note that this could be improved in order to catch additional cases.
		return false
	}
}

// isConditionalResource returns true if the given resource is a counted resource that is instantiated exactly 0 or 1
// times.
func isConditionalResource(r *ResourceNode) bool {
	countExpr, ok := r.Count.(BoundExpr)
	return ok && isBooleanValue(countExpr)
}

// MarkConditionalResources finds all resources and data sources with a count that is known to be either 0 or 1
// (this includes counts that are coerced from boolean values).
func MarkConditionalResources(g *Graph) map[*ResourceNode]bool {
	conditionalResources := make(map[*ResourceNode]bool)
	for _, r := range g.Resources {
		if isConditionalResource(r) {
			conditionalResources[r] = true
		}
	}
	return conditionalResources
}

// isBooleanLiteral checks to see if a BoundExpr is a boolean literal. If so, it returns the BoundLiteral.
func isBooleanLiteral(expr BoundExpr) (*BoundLiteral, bool) {
	if lit, ok := expr.(*BoundLiteral); ok {
		if _, ok = lit.Value.(bool); ok {
			return lit, ok
		}
	}
	return nil, false
}

// SimplifyBooleanExpressions recursively simplifies conditional and literal expressions with statically known values.
//
// Note that this will convert a top-level literal that is coerceable to a boolean into a boolean literal, so this
// function should only be called if the resulting expression can be boolean-typed.
func SimplifyBooleanExpressions(expr BoundExpr) BoundExpr {
	switch expr := expr.(type) {
	case *BoundConditional:
		// If the conditional expression simplifies to a boolean literal, we can replace the entire conditional
		// expression with the literal value.
		condExpr := SimplifyBooleanExpressions(expr.CondExpr)
		if lit, ok := isBooleanLiteral(condExpr); ok {
			return lit
		}

		trueExpr := SimplifyBooleanExpressions(expr.TrueExpr)
		falseExpr := SimplifyBooleanExpressions(expr.FalseExpr)

		trueLit, hasTrueLit := isBooleanLiteral(trueExpr)
		falseLit, hasFalseLit := isBooleanLiteral(falseExpr)

		if hasTrueLit && hasFalseLit {
			trueVal, falseVal := trueLit.Value.(bool), falseLit.Value.(bool)

			// If both legs of the conditional resolve to boolean literals and those literals are the same value, we
			// can simplify the entire expression to the resolved literal.
			if trueVal == falseVal {
				return trueLit
			}

			// Otherwise, we can simplify to the condition expression itself if the true leg resolves to the literal
			// "true" and the false leg resolves to the literal "false".
			if trueVal && !falseVal {
				return condExpr
			}
		}

		expr.CondExpr, expr.TrueExpr, expr.FalseExpr = condExpr, trueExpr, falseExpr
		return expr
	case *BoundLiteral:
		// If this literal
		if boolLit, ok := coerceLiteral(expr, expr.Type(), TypeBool); ok {
			return boolLit
		}
		return expr
	default:
		return expr
	}
}

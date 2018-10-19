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
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"
)

// The applyRewriter is responsible for transforming expressions involving Pulumi output properties into a call to the
// __apply intrinsic and replacing the output properties with appropriate calls to the __applyArg intrinsic.
type applyRewriter struct {
	root      BoundExpr
	applyArgs []BoundExpr
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

	return &BoundCall{
		HILNode:  &ast.Call{Func: "__applyArg"},
		ExprType: n.Type().ElementType(),
		Args:     []BoundExpr{&BoundLiteral{ExprType: TypeNumber, Value: idx}},
	}, nil
}

// rewriteRoot replaces the root node in a bound expression with a call to the __apply intrinsic if necessary.
func (r *applyRewriter) rewriteRoot(n BoundExpr) (BoundNode, error) {
	contract.Require(n == r.root, "n")

	// Clear the root context so that future calls to enterNode recognize new expression roots.
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

			// If the argument to this parameter is an archive resource, strip the field off of the variable access and
			// pass the variable directly.
			isArchiveResource := false
			if v, ok := e.(*BoundVariableAccess); ok {
				if r, ok := v.ILNode.(*ResourceNode); ok && r.Provider.Config.Name == "archive" {
					v.Elements = nil
					isArchiveResource = true
				}
			}

			if !isArchiveResource {
				builtin := "__asset"
				if asset.Kind == tfbridge.FileArchive || asset.Kind == tfbridge.BytesArchive {
					builtin = "__archive"
				}

				m.Elements[k] = &BoundCall{
					HILNode:  &ast.Call{Func: builtin},
					ExprType: TypeUnknown,
					Args:     []BoundExpr{e},
				}
			}

			if asset.HashField != "" {
				delete(m.Elements, asset.HashField)
			}
		}

		return m, nil
	}

	return VisitBoundNode(n, IdentityVisitor, rewriter)
}

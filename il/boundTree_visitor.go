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

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
)

// A BoundNodeVisitor is a function that visits and optionally replaces a node in a bound property tree.
type BoundNodeVisitor func(n BoundNode) (BoundNode, error)

// IdentityVisitor is a BoundNodeVisitor that returns the input node unchanged.
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

func visitBoundError(n *BoundError, pre, post BoundNodeVisitor) (BoundNode, error) {
	valueNode, err := VisitBoundNode(n.Value, pre, post)
	if err != nil {
		return nil, err
	}
	n.Value = valueNode
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
	n.Elements = exprs
	return post(n)
}

func visitBoundMapProperty(n *BoundMapProperty, pre, post BoundNodeVisitor) (BoundNode, error) {
	// Sort the keys to ensure a deterministic visitation order.
	var keys []string
	for k := range n.Elements {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		ee, err := VisitBoundNode(n.Elements[k], pre, post)
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
	n.Exprs = exprs
	return post(n)
}

func visitBoundPropertyValue(n *BoundPropertyValue, pre, post BoundNodeVisitor) (BoundNode, error) {
	value, err := VisitBoundNode(n.Value, pre, post)
	if err != nil {
		return nil, err
	}
	n.Value = value
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

// VisitBoundNode visits each node in a property tree using the given pre- and post-order visitors. If the preorder
// visitor returns a new node, that node's descendents will be visited. This function returns the result of the
// post-order visitor. If any visitor returns an error, the walk halts and that error is returned.
func VisitBoundNode(n BoundNode, pre, post BoundNodeVisitor) (BoundNode, error) {
	if n == nil {
		return nil, nil
	}

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
	case *BoundError:
		return visitBoundError(n, pre, post)
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
	case *BoundPropertyValue:
		return visitBoundPropertyValue(n, pre, post)
	case *BoundVariableAccess:
		return post(n)
	default:
		contract.Failf("unexpected node type in visitBoundExpr: %T", n)
		return nil, errors.Errorf("unexpected node type in visitBoundExpr: %T", n)
	}
}

// VisitBoundExpr visits each node in an expression tree using the given pre- and post-order visitors. Its behavior is
// identical to that of VisitBoundNode, but it requires that the given visitors return BoundExpr values.
func VisitBoundExpr(n BoundExpr, pre, post BoundNodeVisitor) (BoundExpr, error) {
	nn, err := VisitBoundNode(n, pre, post)
	if err != nil || nn == nil {
		return nil, err
	}
	return nn.(BoundExpr), nil
}

// VisitAllProperties visits all property nodes in the graph using the given pre- and post-order visitors.
func VisitAllProperties(m *Graph, pre, post BoundNodeVisitor) error {
	for _, n := range m.Modules {
		if _, err := VisitBoundNode(n.Properties, pre, post); err != nil {
			return err
		}
	}
	for _, n := range m.Providers {
		if _, err := VisitBoundNode(n.Properties, pre, post); err != nil {
			return err
		}
	}
	for _, n := range m.Resources {
		if n.Count != nil {
			if _, err := VisitBoundNode(n.Count, pre, post); err != nil {
				return err
			}
		}
		if _, err := VisitBoundNode(n.Properties, pre, post); err != nil {
			return err
		}
	}
	for _, n := range m.Outputs {
		if _, err := VisitBoundNode(n.Value, pre, post); err != nil {
			return err
		}
	}
	for _, n := range m.Locals {
		if _, err := VisitBoundNode(n.Value, pre, post); err != nil {
			return err
		}
	}
	for _, n := range m.Variables {
		if _, err := VisitBoundNode(n.DefaultValue, pre, post); err != nil {
			return err
		}
	}
	return nil
}

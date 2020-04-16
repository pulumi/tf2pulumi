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

package nodejs

import (
	"path/filepath"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
)

// lowerToLiterals lowers certain elements--namely Module and Root path references--to bound literals. This allows the
// code generator to fold these expressions into template literals as necessary.
func (g *generator) lowerToLiterals(prop il.BoundNode) (il.BoundNode, error) {
	rewriter := func(n il.BoundNode) (il.BoundNode, error) {
		v, ok := n.(*il.BoundVariableAccess)
		if !ok {
			return n, nil
		}

		pv, ok := v.TFVar.(*config.PathVariable)
		if !ok {
			return n, nil
		}

		switch pv.Type {
		case config.PathValueModule:
			path := g.module.Path

			// Attempt to make this path relative to that of the root module.
			rel, err := filepath.Rel(g.rootPath, path)
			if err == nil {
				path = rel
			}

			return &il.BoundLiteral{ExprType: il.TypeString, Value: path}, nil
		case config.PathValueRoot:
			// NOTE: this might not be the most useful or correct value. Might want Node's __directory or similar.
			return &il.BoundLiteral{ExprType: il.TypeString, Value: "."}, nil
		default:
			return n, nil
		}
	}

	return il.VisitBoundNode(prop, il.IdentityVisitor, rewriter)
}

// canLiftVariableAccess returns true if this variable access expression can be lifted. Any variable access expression
// that does not contain references to potentially-undefined values (e.g. optional fields of a resource) can be lifted.
func (g *generator) canLiftVariableAccess(v *il.BoundVariableAccess) bool {
	sch, elements := g.getNestedPropertyAccessElementInfo(v)

	for _, e := range elements {
		if sch.TF != nil && sch.TF.Optional {
			return false
		}
		sch = sch.PropertySchemas(e)
	}
	return true
}

// parseProxyApply attempts to match the given parsed apply against the pattern (call __applyArg 0). If the call
// matches, it returns the BoundVariableAccess that corresponds to argument zero, which can then be generated as a
// proxied apply call.
func (g *generator) parseProxyApply(args []*il.BoundVariableAccess, then il.BoundExpr) (*il.BoundVariableAccess, bool) {
	if len(args) != 1 {
		return nil, false
	}

	thenCall, ok := then.(*il.BoundCall)
	if !ok || thenCall.Func != il.IntrinsicApplyArg {
		return nil, false
	}

	argIndex := il.ParseApplyArgCall(thenCall)
	if argIndex != 0 {
		return nil, false
	}

	v := args[argIndex]
	if !g.canLiftVariableAccess(v) {
		return nil, false
	}

	return v, true
}

// hasApplyArgDescendant returns true if the given BoundExpr has any descendant that is a call to __applyArg. This is a
// helper for parseInterpolate.
func hasApplyArgDescendant(expr il.BoundExpr) bool {
	has := false
	_, err := il.VisitBoundNode(expr, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if c, ok := n.(*il.BoundCall); ok && c.Func == il.IntrinsicApplyArg {
			has = true
		}
		return n, nil
	})
	contract.Assert(err == nil)
	return has
}

// parseInterpolate attempts to match the given parsed apply against the pattern (output /* mix of expressions and
// calls to __applyArg).
//
// A legal expression for the match is any expression that does not contain any calls to __applyArg: an expression that
// does contain such calls requires an apply.
//
// If the call matches, parseInterpolate returns an appropriate call to the __interpolate intrinsic with a mix of
// expressions and variable accesses that correspond to the __applyArg calls.
func (g *generator) parseInterpolate(args []*il.BoundVariableAccess, then il.BoundExpr) (*il.BoundCall, bool) {
	thenOutput, ok := then.(*il.BoundOutput)
	if !ok {
		return nil, false
	}

	exprs := make([]il.BoundExpr, len(thenOutput.Exprs))
	for i, expr := range thenOutput.Exprs {
		call, isCall := expr.(*il.BoundCall)
		switch {
		case isCall && call.Func == il.IntrinsicApplyArg:
			v := args[il.ParseApplyArgCall(call)]
			if !g.canLiftVariableAccess(v) {
				return nil, false
			}
			exprs[i] = v
		case !hasApplyArgDescendant(expr):
			exprs[i] = expr
		default:
			return nil, false
		}
	}

	return newInterpolateCall(exprs), true
}

// lowerProxyApplies lowers certain calls to the apply intrinsic into proxied property accesses and/or calls to the
// pulumi.interpolate function. Concretely, this boils down to rewriting the following shapes
// - (call __apply (resource variable access) (call __applyArg 0))
// - (call __apply (resource variable access 0) ... (resource variable access n)
//       (output /* some mix of expressions and calls to __applyArg))
// into (respectively)
// - (resource variable access)
// - (call __interpolate /* mix of literals and variable accesses that correspond to the __applyArg calls)
//
// The generated code requires that the target version of `@pulumi/pulumi` supports output proxies.
func (g *generator) lowerProxyApplies(prop il.BoundNode) (il.BoundNode, error) {
	rewriter := func(n il.BoundNode) (il.BoundNode, error) {
		// Ignore the node if it is not a call to the apply intrinsic.
		apply, ok := n.(*il.BoundCall)
		if !ok || apply.Func != il.IntrinsicApply {
			return n, nil
		}

		// Parse the apply call.
		args, then := il.ParseApplyCall(apply)

		// Attempt to match (call __apply (rvar) (call __applyArg 0))
		if v, ok := g.parseProxyApply(args, then); ok {
			return v, nil
		}

		// Attempt to match (call __apply (rvar 0) ... (rvar n) (output /* mix of literals and calls to __applyArg)
		if v, ok := g.parseInterpolate(args, then); ok {
			return v, nil
		}

		return n, nil
	}
	return il.VisitBoundNode(prop, il.IdentityVisitor, rewriter)
}

// Copyright 2016-2019, Pulumi Corporation.
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

package python

import (
	"github.com/pulumi/tf2pulumi/il"
)

type trivialApplyRewriter struct{}

func (t trivialApplyRewriter) rewriteNode(n il.BoundNode) (il.BoundNode, error) {
	if e, ok := n.(*il.BoundCall); ok && e.Func == il.IntrinsicApply {
		args, access := il.ParseApplyCall(e)
		if len(args) != 1 {
			return n, nil
		}
		if applyArg, ok := access.(*il.BoundCall); ok && applyArg.Func == il.IntrinsicApplyArg {
			index := il.ParseApplyArgCall(applyArg)
			if index != 0 {
				// Not sure what this is - leave it alone.
				return n, nil
			}

			return args[0], nil
		}
	}
	return n, nil
}

// RewriteTrivialApplies rewrites all applies within the bound node and its children to use "sugared" syntax if the
// apply itself is trivial. A trivial apply is an apply (a sequence of __apply and __applyArg intrinsics) that consist
// of simply reading a field off of an output-typed object.
//
// The Python SDK has special syntax sugar for this pattern that alleviates the need to write this apply by hand, so
// this pass elides them entirely.
func RewriteTrivialApplies(n il.BoundNode) (il.BoundNode, error) {
	rewriter := trivialApplyRewriter{}
	return il.VisitBoundNode(n, il.IdentityVisitor, rewriter.rewriteNode)
}

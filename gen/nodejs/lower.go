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

	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/tf2pulumi/il"
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
			path := g.module.Tree.Config().Dir

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

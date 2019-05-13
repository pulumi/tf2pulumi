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
	"github.com/hashicorp/hil/ast"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

const (
	// intrinsicDataSource is the name of the data source intrinsic.
	intrinsicDataSource = "__dataSource"
	// inttrinsicInterpolate is the name of the interpolate intrinsic.
	intrinsicInterpolate = "__interpolate"
)

// newDataSourceCall creates a new call to the data source intrinsic that represents an invocation of the specified
// data source function with the given input properties.
func newDataSourceCall(functionName string, inputs il.BoundNode) *il.BoundCall {
	return &il.BoundCall{
		HILNode:  &ast.Call{Func: intrinsicDataSource},
		ExprType: il.TypeMap,
		Args: []il.BoundExpr{
			&il.BoundLiteral{
				ExprType: il.TypeString,
				Value:    functionName,
			},
			&il.BoundPropertyValue{
				NodeType: il.TypeMap,
				Value:    inputs,
			},
		},
	}
}

// parseDataSourceCall extracts the name of the data source function and the input properties for its invocation from
// a call to the data source intrinsic.
func parseDataSourceCall(c *il.BoundCall) (function string, inputs il.BoundNode) {
	contract.Assert(c.HILNode.Func == intrinsicDataSource)
	return c.Args[0].(*il.BoundLiteral).Value.(string), c.Args[1].(*il.BoundPropertyValue).Value
}

// newInterpolateCall creates a new call to the interpolate intrinsic that represents a template literal that uses the
// pulumi.interpolate function.
func newInterpolateCall(args []il.BoundExpr) *il.BoundCall {
	return &il.BoundCall{
		HILNode:  &ast.Call{Func: intrinsicInterpolate},
		ExprType: il.TypeString.OutputOf(),
		Args:     args,
	}
}

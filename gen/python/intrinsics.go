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

package python

import (
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/pulumi/tf2pulumi/il"
)

const (
	intrinsicDataSource = "__dataSource"
	intrinsicResource   = "__resource"
)

func newResourceCall(resourceType, resourceName string, inputs *il.BoundMapProperty) *il.BoundCall {
	return &il.BoundCall{
		Func:     intrinsicResource,
		ExprType: il.TypeMap,
		Args: []il.BoundExpr{
			&il.BoundLiteral{
				ExprType: il.TypeString,
				Value:    resourceType,
			},
			&il.BoundLiteral{
				ExprType: il.TypeString,
				Value:    resourceName,
			},
			&il.BoundPropertyValue{
				NodeType: il.TypeMap,
				Value:    inputs,
			},
		},
	}
}

func newDataSourceCall(functionName string, inputs *il.BoundMapProperty) *il.BoundCall {
	return &il.BoundCall{
		Func:     intrinsicDataSource,
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
func parseDataSourceCall(c *il.BoundCall) (function string, inputs *il.BoundMapProperty) {
	contract.Assert(c.Func == intrinsicDataSource)
	return c.Args[0].(*il.BoundLiteral).Value.(string), c.Args[1].(*il.BoundPropertyValue).Value.(*il.BoundMapProperty)
}

// parseResourceCall extracts the type of the resource, the name of the resource, and the resource's input properties
// from a call to the resource intrinsic.
func parseResourceCall(c *il.BoundCall) (resource, name string, inputs *il.BoundMapProperty) {
	contract.Assert(c.Func == intrinsicResource)
	return c.Args[0].(*il.BoundLiteral).Value.(string),
		c.Args[1].(*il.BoundLiteral).Value.(string),
		c.Args[2].(*il.BoundPropertyValue).Value.(*il.BoundMapProperty)
}

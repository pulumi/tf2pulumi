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
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
)

const (
	// IntrinsicApply is the name of the apply intrinsic.
	IntrinsicApply = "__apply"
	// IntrinsicApplyArg is the name of the apply arg intrinsic.
	IntrinsicApplyArg = "__applyArg"
	// IntrinsicArchive is the name of the archive intrinsic.
	IntrinsicArchive = "__archive"
	// IntrinsicAsset is the name of the asset intrinsic.
	IntrinsicAsset = "__asset"
	// IntrinsicCoerce is the name of the coerce intrinsic.
	IntrinsicCoerce = "__coerce"
	// IntrinsicGetStack is the name of the get stack intrinsic.
	IntrinsicGetStack = "__getStack"
)

// NewApplyCall returns a new IL tree that represents a call to IntrinsicApply.
func NewApplyCall(args []*BoundVariableAccess, then BoundExpr) *BoundCall {
	exprs := make([]BoundExpr, len(args)+1)
	for i, a := range args {
		exprs[i] = a
	}
	exprs[len(exprs)-1] = then

	return &BoundCall{
		Func:     IntrinsicApply,
		ExprType: then.Type().OutputOf(),
		Args:     exprs,
	}
}

// ParseApplyCall extracts the apply arguments and the continuation from a call to the apply intrinsic.
func ParseApplyCall(c *BoundCall) (applyArgs []*BoundVariableAccess, then BoundExpr) {
	contract.Assert(c.Func == IntrinsicApply)

	args := make([]*BoundVariableAccess, len(c.Args)-1)
	for i, a := range c.Args[:len(args)] {
		args[i] = a.(*BoundVariableAccess)
	}

	return args, c.Args[len(c.Args)-1]
}

// NewApplyArgCall returns a new IL tree that represents a call to IntrinsicApplyArg.
func NewApplyArgCall(argIndex int, argType Type) *BoundCall {
	contract.Assert(!argType.IsOutput())
	return &BoundCall{
		Func:     IntrinsicApplyArg,
		ExprType: argType,
		Args:     []BoundExpr{&BoundLiteral{ExprType: TypeNumber, Value: argIndex}},
	}
}

// ParseapplyArgCall extracts the argument index from a call to the apply arg intrinsic.
func ParseApplyArgCall(c *BoundCall) int {
	contract.Assert(c.Func == IntrinsicApplyArg)
	return c.Args[0].(*BoundLiteral).Value.(int)
}

// NewArchiveCall creates a call to IntrinsicArchive.
func NewArchiveCall(arg BoundExpr) *BoundCall {
	return &BoundCall{
		Func:     IntrinsicArchive,
		ExprType: TypeUnknown,
		Args:     []BoundExpr{arg},
	}
}

// ParseArchiveCall extracts the single argument expression from a call to the archive intrinsic.
func ParseArchiveCall(c *BoundCall) (arg BoundExpr) {
	contract.Assert(c.Func == IntrinsicArchive)
	return c.Args[0]
}

// NewAssetCall creates a call to IntrinsicArchive.
func NewAssetCall(arg BoundExpr) *BoundCall {
	return &BoundCall{
		Func:     IntrinsicAsset,
		ExprType: TypeUnknown,
		Args:     []BoundExpr{arg},
	}
}

// ParseAssetCall extracts the single argument expression from a call to the asset intrinsic.
func ParseAssetCall(c *BoundCall) (arg BoundExpr) {
	contract.Assert(c.Func == IntrinsicAsset)
	return c.Args[0]
}

// NewCoerceCall creates a call to IntrisicCoerce, which is used to represent the coercion of a value from one type to
// another.
func NewCoerceCall(value BoundExpr, toType Type) *BoundCall {
	return &BoundCall{
		Func:     IntrinsicCoerce,
		ExprType: toType,
		Args:     []BoundExpr{value},
	}
}

// ParseCoerceCall extracts the value being coerced and the type to which it is being coerced from a call to the coerce
// intrinsic.
func ParseCoerceCall(c *BoundCall) (value BoundExpr, toType Type) {
	contract.Assert(c.Func == IntrinsicCoerce)
	return c.Args[0], c.ExprType
}

// NewGetStackCall creates a call to IntrinsicGetStack.
func NewGetStackCall() *BoundCall {
	return &BoundCall{Func: IntrinsicGetStack, ExprType: TypeString}
}

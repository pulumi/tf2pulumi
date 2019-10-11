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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntrinsicApply(t *testing.T) {
	args := []*BoundVariableAccess{
		{},
		{},
		{},
	}
	then := &BoundLiteral{}

	c := NewApplyCall(args, then)
	assert.Equal(t, IntrinsicApply, c.Func)
	assert.Equal(t, then.Type().OutputOf(), c.ExprType)
	assert.Equal(t, len(args)+1, len(c.Args))

	args2, then2 := ParseApplyCall(c)
	assert.EqualValues(t, args, args2)
	assert.Equal(t, then, then2)
}

func TestIntrinsicApplyArg(t *testing.T) {
	idx, typ := 3, TypeString

	c := NewApplyArgCall(idx, typ)
	assert.Equal(t, IntrinsicApplyArg, c.Func)
	assert.Equal(t, typ, c.Type())
	assert.Equal(t, 1, len(c.Args))

	assert.Equal(t, idx, ParseApplyArgCall(c))
}

func TestIntrinsicArchive(t *testing.T) {
	arg := &BoundLiteral{}

	c := NewArchiveCall(arg)
	assert.Equal(t, IntrinsicArchive, c.Func)
	assert.Equal(t, TypeUnknown, c.Type())
	assert.Equal(t, 1, len(c.Args))

	assert.Equal(t, arg, ParseArchiveCall(c))
}

func TestIntrinsicAsset(t *testing.T) {
	arg := &BoundLiteral{}

	c := NewAssetCall(arg)
	assert.Equal(t, IntrinsicAsset, c.Func)
	assert.Equal(t, TypeUnknown, c.Type())
	assert.Equal(t, 1, len(c.Args))

	assert.Equal(t, arg, ParseAssetCall(c))
}

func TestIntrinsicCoerce(t *testing.T) {
	value, toType := &BoundLiteral{}, TypeNumber

	c := NewCoerceCall(value, toType)
	assert.Equal(t, IntrinsicCoerce, c.Func)
	assert.Equal(t, toType, c.Type())
	assert.Equal(t, 1, len(c.Args))

	value2, toType2 := ParseCoerceCall(c)
	assert.Equal(t, value, value2)
	assert.Equal(t, toType, toType2)
}

func TestIntrinsicGetStack(t *testing.T) {
	c := NewGetStackCall()
	assert.Equal(t, IntrinsicGetStack, c.Func)
	assert.Equal(t, TypeString, c.Type())
	assert.Equal(t, 0, len(c.Args))
}

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
	"testing"

	"github.com/pulumi/tf2pulumi/il"
	"github.com/stretchr/testify/assert"
)

func TestIntrinsicDataSource(t *testing.T) {
	function := "aws.getFunction"
	inputs := &il.BoundMapProperty{}
	optionsBag := ", {}"

	c := newDataSourceCall(function, inputs, optionsBag)
	assert.Equal(t, intrinsicDataSource, c.HILNode.Func)
	assert.Equal(t, il.TypeMap, c.ExprType)
	assert.Equal(t, 3, len(c.Args))

	function2, inputs2, optionsBag2 := parseDataSourceCall(c)
	assert.Equal(t, function, function2)
	assert.Equal(t, inputs, inputs2)
	assert.Equal(t, optionsBag, optionsBag2)
}

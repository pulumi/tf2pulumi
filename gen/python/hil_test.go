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
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/il"
)

func runGen(node il.BoundNode) string {
	var buf bytes.Buffer
	g := &generator{
		projectName: "test",
		w:           &buf,
	}

	g.gen(&buf, node)
	return buf.String()
}

func TestHilLiteralLowerBool(t *testing.T) {
	cases := []struct {
		Value bool
		Gen   string
	}{
		{Value: true, Gen: "True"},
		{Value: false, Gen: "False"},
	}

	for _, test := range cases {
		t.Run(test.Gen, func(t *testing.T) {
			node := &il.BoundLiteral{
				ExprType: il.TypeBool,
				Value:    test.Value,
			}

			out := runGen(node)
			assert.Equal(t, test.Gen, out)
		})
	}
}

func TestHilLiteralLowerNumber(t *testing.T) {
	cases := []struct {
		Value float64
		Gen   string
	}{
		{Value: 2, Gen: "2"},
		{Value: 2.1, Gen: "2.1"},
		{Value: 2.0, Gen: "2"},
		{Value: 4299.12, Gen: "4299.12"},
	}

	for _, test := range cases {
		t.Run(test.Gen, func(t *testing.T) {
			node := &il.BoundLiteral{
				ExprType: il.TypeNumber,
				Value:    test.Value,
			}

			out := runGen(node)
			assert.Equal(t, test.Gen, out)
		})
	}
}

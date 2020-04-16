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
	"fmt"
	"io"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
)

const (
	// nyiHelper is the code for a NYI helper function that tf2pulumi will emit if it needs to signal a runtime error.
	nyiHelper = `

def tf2pulumi_nyi(reason):
    """
    Raises an exception due to a tf2pulumi NYI error.
    """
    raise Exception("nyi: " + reason)

`
)

func (g *generator) GenArithmetic(w io.Writer, v *il.BoundArithmetic) {
	g.genNYI(w, "arithmetic")
}

func (g *generator) GenCall(w io.Writer, v *il.BoundCall) {
	switch v.Func {
	case intrinsicDataSource:
		g.genDataSourceCall(w, v)
	case intrinsicResource:
		g.genResourceCall(w, v)
	case il.IntrinsicApply:
		g.genApply(w, v)
	default:
		g.genNYI(w, "call")
	}
}

func (g *generator) genDataSourceCall(w io.Writer, v *il.BoundCall) {
	functionName, inputs := parseDataSourceCall(v)

	// Like resources, Python projects property input maps as keyword arguments on the data source function itself.
	// The name of the data source function is functionName above - we've already calculated the Python name for it.
	g.Fgenf(w, "%s(", functionName)
	sortedElements := gen.SortedKeys(inputs.Elements)
	for i, key := range sortedElements {
		value := inputs.Elements[key]
		g.Fgenf(w, "%s=%v", key, value)
		if i != len(sortedElements)-1 {
			g.Fgen(w, ", ")
		}
	}
	g.Fgen(w, ")")
}

func (g *generator) genResourceCall(w io.Writer, v *il.BoundCall) {
	resourceType, resourceName, inputs := parseResourceCall(v)
	g.Fgenf(w, "%s(%q, ", resourceType, resourceName)
	sortedElements := gen.SortedKeys(inputs.Elements)
	for i, key := range sortedElements {
		value := inputs.Elements[key]
		g.Fgenf(w, "%s=%v", key, value)
		if i != len(sortedElements)-1 {
			g.Fgen(w, ", ")
		}
	}
	g.Fgen(w, ")")
}

func (g *generator) genApply(w io.Writer, v *il.BoundCall) {
	g.genNYI(w, "nontrivial apply")
}

func (g *generator) GenConditional(w io.Writer, v *il.BoundConditional) {
	g.genNYI(w, "conditionals")
}

func (g *generator) GenIndex(w io.Writer, v *il.BoundIndex) {
	g.genNYI(w, "index")
}

func (g *generator) GenLiteral(w io.Writer, v *il.BoundLiteral) {
	switch v.ExprType {
	case il.TypeBool:
		boolVal := v.Value.(bool)
		if boolVal {
			g.Fgen(w, "True")
		} else {
			g.Fgen(w, "False")
		}
	case il.TypeNumber:
		floatVal := v.Value.(float64)
		if float64(int64(floatVal)) == floatVal {
			g.Fgenf(w, "%d", int64(floatVal))
		} else {
			g.Fgenf(w, "%g", v.Value)
		}
	case il.TypeString:
		g.Fgenf(w, "%q", v.Value.(string))
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", v.ExprType)
	}
}

func (g *generator) GenOutput(w io.Writer, v *il.BoundOutput) {
	g.genNYI(w, "outputs")
}

func (g *generator) GenVariableAccess(w io.Writer, v *il.BoundVariableAccess) {
	switch v.TFVar.(type) {
	case *config.ResourceVariable:
		if v.ILNode == nil {
			g.genNYI(w, "resource variable with no IL node")
			return
		}

		name := g.nodeName(v.ILNode)
		g.Fgenf(w, "%s.%s", name, v.Elements[0])
	default:
		g.genNYI(w, "variables")
	}
}

func (g *generator) GenListProperty(w io.Writer, v *il.BoundListProperty) {
	g.Fgen(w, "[")
	for i, prop := range v.Elements {
		g.Fgen(w, prop)
		if i != len(v.Elements)-1 {
			g.Fgen(w, ", ")
		}
	}
	g.Fgen(w, "]")
}

func (g *generator) GenMapProperty(w io.Writer, v *il.BoundMapProperty) {
	g.Fgen(w, "{")
	sortedElements := gen.SortedKeys(v.Elements)
	for i, key := range sortedElements {
		value := v.Elements[key]
		g.Fgenf(w, "%q: %s", key, value)
		if i != len(sortedElements)-1 {
			g.Fgen(w, ", ")
		}
	}
	g.Fgen(w, "}")
}

func (g *generator) GenPropertyValue(w io.Writer, v *il.BoundPropertyValue) {
	g.Fgen(w, v.Value)
}

func (g *generator) GenError(w io.Writer, v *il.BoundError) {
	g.genNYI(w, "errors")
}

// genNYI emits an expression that throws at runtime with a message indicating what wasn't implemented. The written
// string is an expression, unlike the `raise` statement in Python.
func (g *generator) genNYI(w io.Writer, reason string) {
	g.needNYIHelper = true
	_, err := fmt.Fprintf(w, `tf2pulumi_nyi(%q)`, reason)
	contract.IgnoreError(err)
}

// genNYIHelper emits the NYI helper, if required.
func (g *generator) genNYIHelper(w io.Writer) {
	if g.needNYIHelper {
		_, err := fmt.Fprintln(w, nyiHelper)
		contract.IgnoreError(err)
	}
}

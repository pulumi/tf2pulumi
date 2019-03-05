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

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
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
	g.genNYI(w, "call")
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
		g.genNYI(w, "string literals")
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", v.ExprType)
	}
}

func (g *generator) GenOutput(w io.Writer, v *il.BoundOutput) {
	g.genNYI(w, "outputs")
}

func (g *generator) GenVariableAccess(w io.Writer, v *il.BoundVariableAccess) {
	g.genNYI(w, "variables")
}

func (g *generator) GenListProperty(w io.Writer, v *il.BoundListProperty) {
	g.genNYI(w, "list properties")
}

func (g *generator) GenMapProperty(w io.Writer, v *il.BoundMapProperty) {
	if v.Schemas.TF != nil && v.Schemas.TF.Type == schema.TypeMap {
		g.genNYI(w, "exact map properties")
		return
	}

	if len(v.Elements) == 0 {
		return
	}

	// Unlike the Node backend, Python resources accept keyword arguments for each input property. If we're being
	// requested to instantiate a resource map (i.e. not a field whose schema type is TypeMap), emit a comma-separated
	// list of keyword arguments for a function.
	//
	// Note that this is not a valid expression normally, so we must be in a "call" parse context in order for this to
	// parse correctly.
	for _, key := range gen.SortedKeys(v.Elements) {
		value := v.Elements[key]
		// TODO(swgillespie) emit leading comments
		g.Fgenf(w, ", %s=%v", key, value)
		// TODO(swgillespie) emit trailing comments
	}
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

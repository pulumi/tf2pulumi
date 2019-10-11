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
	"fmt"
	"io"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// genListProperty generates code for as single list property.
func (g *generator) GenListProperty(w io.Writer, n *il.BoundListProperty) {
	switch len(n.Elements) {
	case 0:
		g.Fgen(w, "[]")
	case 1:
		// We can ignore comments in this case: the comment extractor will never associate comments with a
		// single-element list.
		v := n.Elements[0]
		if v.Type().IsList() {
			// TF flattens list elements that are themselves lists into the parent list.
			//
			// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
			// only knowable at runtime and will require a helper.
			g.Fgenf(w, "%v", v)
		} else {
			g.Fgenf(w, "[%v]", v)
		}
	default:
		g.Fgen(w, "[")
		g.Indented(func() {
			for _, v := range n.Elements {
				g.Fgenf(w, "\n")
				g.genLeadingComment(w, v.Comments())
				g.Fgenf(w, "%s", g.Indent)

				// TF flattens list elements that are themselves lists into the parent list.
				//
				// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
				// only knowable at runtime and will require a helper.
				if v.Type().IsList() {
					g.Fgen(w, "...")
				}
				g.Fgenf(w, "%v,", v)

				g.genTrailingComment(w, v.Comments())
			}
		})
		g.Fgen(w, "\n", g.Indent, "]")
	}
}

// genMapProperty generates code for a single map property.
func (g *generator) GenMapProperty(w io.Writer, n *il.BoundMapProperty) {
	if len(n.Elements) == 0 {
		g.Fgen(w, "{}")
	} else {
		useExactKeys := n.Schemas.TF != nil && n.Schemas.TF.Type == schema.TypeMap

		g.Fgen(w, "{")
		g.Indented(func() {
			for _, k := range gen.SortedKeys(n.Elements) {
				v := n.Elements[k]

				g.Fgenf(w, "\n")
				g.genLeadingComment(w, v.Comments())

				propSch, key := n.Schemas.PropertySchemas(k), k
				if !useExactKeys {
					key = tsName(k, propSch.TF, propSch.Pulumi, true)
				} else if !isLegalIdentifier(key) {
					key = fmt.Sprintf("%q", key)
				}
				g.Fgenf(w, "%s%s: %v,", g.Indent, key, v)

				g.genTrailingComment(w, v.Comments())
			}
		})
		g.Fgen(w, "\n", g.Indent, "}")
	}
}

package nodejs

import (
	"io"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/il"
)

func (g *Generator) genListProperty(w io.Writer, n *il.BoundListProperty) {
	if len(n.Elements) == 0 {
		g.gen(w, "[]")
	} else {
		g.gen(w, "[")
		g.indented(func() {
			for _, v := range n.Elements {
				// TF flattens list elements that are themselves lists into the parent list.
				//
				// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
				// only knowable at runtime and will require a helper.
				if v.Type().IsList() {
					g.gen(w, "...")
				}
				g.genf(w, "\n%s%v,", g.indent, v)
			}
		})
		g.gen(w, "\n", g.indent, "]")
	}
}

func (g *Generator) genMapProperty(w io.Writer, n *il.BoundMapProperty) {
	if len(n.Elements) == 0 {
		g.gen(w, "{}")
	} else {
		g.gen(w, "{")
		g.indented(func() {
			for _, k := range gen.SortedKeys(n.Elements) {
				propSch := n.Schemas.PropertySchemas(k)
				g.genf(w, "\n%s%s: %v,", g.indent, tsName(k, propSch.TF, propSch.Pulumi, true), n.Elements[k])
			}
		})
		g.gen(w, "\n", g.indent, "}")
	}
}

package nodejs

import (
	"io"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/il"
)

func (g *Generator) genListProperty(w io.Writer, n *il.BoundListProperty) {
	elemType := n.Schemas.ElemSchemas().Type()

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
				g.gen(w, "\n", g.indent)
				g.genCoercion(w, v, elemType)
				g.gen(w, ",")
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
				v := n.Elements[k]

				propSch := n.Schemas.PropertySchemas(k)
				g.gen(w, "\n", g.indent, tsName(k, propSch.TF, propSch.Pulumi, true), ": ")
				g.genCoercion(w, v, propSch.Type())
				g.gen(w, ",")
			}
		})
		g.gen(w, "\n", g.indent, "}")
	}
}

func (g *Generator) genCoercion(w io.Writer, n il.BoundNode, toType il.Type) {
	// TODO: we really need dynamic coercions here.
	if n.Type() == toType {
		g.gen(w, n)
		return
	}

	switch n.Type() {
	case il.TypeBool:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%v\"", lit.Value)
			} else {
				g.genf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeNumber:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "\"%f\"", lit.Value)
			} else {
				g.genf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeString:
		switch toType {
		case il.TypeBool:
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.genf(w, "%v", lit.Value.(string) == "true")
			} else {
				g.genf(w, "(%v === \"true\")", n)
			}
			return
		case il.TypeNumber:
			g.genf(w, "Number.parseFloat(%v)", n)
			return
		}
	}

	// If we get here, we weren't able to genereate a coercion. Just generate the node. This is questionable behavior
	// at best.
	g.gen(w, n)
}

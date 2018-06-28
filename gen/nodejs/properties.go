package nodejs

import (
	"bytes"
	"strconv"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/il"
)

type propertyGenerator struct {
	w      *bytes.Buffer
	hil    *hilGenerator
	indent string
}

func (g *propertyGenerator) indented(f func()) {
	g.indent += "    "
	f()
	g.indent = g.indent[:len(g.indent)-4]
}

func (g *propertyGenerator) genListProperty(n *il.BoundListProperty) {
	elemType := n.Schemas.ElemSchemas().Type()

	g.gen("[")
	g.indented(func() {
		for _, v := range n.Elements {
			// TF flattens list elements that are themselves lists into the parent list.
			//
			// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
			// only knowable at runtime and will require a helper.
			if v.Type().IsList() {
				g.gen("...")
			}
			g.gen("\n", g.indent)
			g.genCoercion(v, elemType)
			g.gen(",")
		}
	})
	g.gen("\n", g.indent, "]")
}

func (g *propertyGenerator) genMapProperty(n *il.BoundMapProperty) {
	g.gen("{")
	g.indented(func() {
		for _, k := range gen.SortedKeys(n.Elements) {
			v := n.Elements[k]

			propSch := n.Schemas.PropertySchemas(k)
			g.gen("\n", g.indent, tsName(k, propSch.TF, propSch.Pulumi, true), ": ")
			g.genCoercion(v, propSch.Type())
			g.gen(",")
		}
	})
	g.gen("\n", g.indent, "}")
}

func (g *propertyGenerator) genCoercion(n il.BoundNode, toType il.Type) {
	// TODO: we really need dynamic coercions here.
	if n.Type() == toType {
		g.gen(n)
		return
	}

	switch n.Type() {
	case il.TypeBool:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.gen("\"", strconv.FormatBool(lit.Value.(bool)), "\"")
			} else {
				g.gen("`${", n, "}`")
			}
			return
		}
	case il.TypeNumber:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.gen("\"", strconv.FormatFloat(lit.Value.(float64), 'f', -1, 64), "\"")
			} else {
				g.gen("`${", n, "}`")
			}
			return
		}
	case il.TypeString:
		switch toType {
		case il.TypeBool:
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.gen(strconv.FormatBool(lit.Value.(string) == "true"))
			} else {
				g.gen("(", n, " === \"true\")")
			}
			return
		case il.TypeNumber:
			g.gen("Number.parseFloat(", n, ")")
			return
		}
	}

	// If we get here, we weren't able to genereate a coercion. Just generate the node. This is questionable behavior
	// at best.
	g.gen(n)
}

func (g *propertyGenerator) gen(vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			g.w.WriteString(v)
		case *il.BoundListProperty:
			g.genListProperty(v)
		case *il.BoundMapProperty:
			g.genMapProperty(v)
		default:
			g.hil.gen(v)
		}
	}
}

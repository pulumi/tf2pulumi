package nodejs

import (
	"bytes"
	"strconv"

	"github.com/hashicorp/hil/ast"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"

	"github.com/pgavlin/firewalker/gen"
)

type boundListProperty struct {
	schemas  schemas
	elements []boundNode
}

func (n *boundListProperty) typ() boundType {
	return n.schemas.elemSchemas().boundType().listOf()
}

type boundMapProperty struct {
	schemas  schemas
	elements map[string]boundNode
}

func (n *boundMapProperty) typ() boundType {
	return typeMap
}

type propertyBinder struct {
	hil *hilBinder
}

func (b *propertyBinder) bindListProperty(s []interface{}, sch schemas) (boundNode, error) {
	if tfbridge.IsMaxItemsOne(sch.tf, sch.pulumi) {
		switch len(s) {
		case 0:
			return nil, nil
		case 1:
			return b.bindProperty(s[0], sch.elemSchemas())
		default:
			return nil, errors.Errorf("expected at most one item in list")
		}
	}

	elements := make([]boundNode, 0, len(s))
	for _, v := range s {
		elem, err := b.bindProperty(v, sch.elemSchemas())
		if err != nil {
			return nil, err
		}
		if elem == nil {
			continue
		}
		elements = append(elements, elem)
	}

	// Terraform spreads nested lists into their containing list. If this list is contains exactly one element that is
	// also a list, do the spread now by simply returning the sole element.
	if len(elements) == 1 && elements[0].typ().isList() {
		return elements[0], nil
	}

	return &boundListProperty{schemas: sch, elements: elements}, nil
}

func (b *propertyBinder) bindMapProperty(m map[string]interface{}, sch schemas) (boundNode, error) {
	elements := make(map[string]boundNode)
	for k, v := range m {
		bv, err := b.bindProperty(v, sch.propertySchemas(k))
		if err != nil {
			return nil, err
		}
		if bv == nil {
			continue
		}
		elements[k] = bv
	}

	return &boundMapProperty{schemas: sch, elements: elements}, nil
}

func (b *propertyBinder) bindProperty(v interface{}, sch schemas) (boundNode, error) {
	switch v := v.(type) {
	case bool:
		return &boundLiteral{exprType: typeBool, value: v}, nil
	case float64:
		return &boundLiteral{exprType: typeNumber, value: v}, nil
	case string:
		return &boundLiteral{exprType: typeString, value: v}, nil
	case ast.Node:
		return b.hil.bindExpr(v)
	case []interface{}:
		return b.bindListProperty(v, sch)
	case map[string]interface{}:
		return b.bindMapProperty(v, sch)
	default:
		return nil, errors.Errorf("unexpected property type %T", v)
	}
}

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

func (g *propertyGenerator) genListProperty(n *boundListProperty) {
	elemType := n.schemas.elemSchemas().boundType()

	g.gen("[")
	g.indented(func() {
		for _, v := range n.elements {
			// TF flattens list elements that are themselves lists into the parent list.
			//
			// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
			// only knowable at runtime and will require a helper.
			if v.typ().isList() {
				g.gen("...")
			}
			g.gen("\n", g.indent)
			g.genCoercion(v, elemType)
			g.gen(",")
		}
	})
	g.gen("\n", g.indent, "]")
}

func (g *propertyGenerator) genMapProperty(n *boundMapProperty) {
	g.gen("{")
	g.indented(func() {
		for _, k := range gen.SortedKeys(n.elements) {
			v := n.elements[k]

			propSch := n.schemas.propertySchemas(k)
			g.gen("\n", g.indent, tsName(k, propSch.tf, propSch.pulumi, true), ": ")
			g.genCoercion(v, propSch.boundType())
			g.gen(",")
		}
	})
	g.gen("\n", g.indent, "}")
}

func (g *propertyGenerator) genCoercion(n boundNode, toType boundType) {
	// TODO: we really need dynamic coercions here.
	if n.typ() == toType {
		g.gen(n)
		return
	}

	switch n.typ() {
	case typeBool:
		if toType == typeString {
			if lit, ok := n.(*boundLiteral); ok {
				g.gen("\"", strconv.FormatBool(lit.value.(bool)), "\"")
			} else {
				g.gen("`${", n, "}`")
			}
			return
		}
	case typeNumber:
		if toType == typeString {
			if lit, ok := n.(*boundLiteral); ok {
				g.gen("\"", strconv.FormatFloat(lit.value.(float64), 'f', -1, 64), "\"")
			} else {
				g.gen("`${", n, "}`")
			}
			return
		}
	case typeString:
		switch toType {
		case typeBool:
			if lit, ok := n.(*boundLiteral); ok {
				g.gen(strconv.FormatBool(lit.value.(string) == "true"))
			} else {
				g.gen("(", n, " === \"true\")")
			}
			return
		case typeNumber:
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
		case *boundListProperty:
			g.genListProperty(v)
		case *boundMapProperty:
			g.genMapProperty(v)
		default:
			g.hil.gen(v)
		}
	}
}

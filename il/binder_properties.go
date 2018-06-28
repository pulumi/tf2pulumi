package il

import (
	"reflect"

	"github.com/hashicorp/hil"
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type propertyBinder struct {
	builder *builder
	hasCountIndex bool
}

func (b *propertyBinder) bindListProperty(s reflect.Value, sch Schemas) (BoundNode, error) {
	contract.Require(s.Kind() == reflect.Slice, "s")

	isMaxItemsOne := sch.TF != nil && sch.TF.Type == schema.TypeMap || tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi)
	if isMaxItemsOne {
		switch s.Len() {
		case 0:
			return nil, nil
		case 1:
			return b.bindProperty(s.Index(0), sch.ElemSchemas())
		default:
			return nil, errors.Errorf("expected at most one item in list")
		}
	}

	elements := make([]BoundNode, 0, s.Len())
	for i := 0; i < s.Len(); i++ {
		elem, err := b.bindProperty(s.Index(i), sch.ElemSchemas())
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
	if len(elements) == 1 && elements[0].Type().IsList() {
		return elements[0], nil
	}

	return &BoundListProperty{Schemas: sch, Elements: elements}, nil
}

func (b *propertyBinder) bindMapProperty(m reflect.Value, sch Schemas) (BoundNode, error) {
	contract.Require(m.Kind() == reflect.Map, "m")

	// Grab the key type and ensure it is of type string
	if m.Type().Key().Kind() != reflect.String {
		return nil, errors.Errorf("unexpected key type %v", m.Type().Key())
	}

	elements := make(map[string]BoundNode)
	for _, k := range m.MapKeys() {
		bv, err := b.bindProperty(m.MapIndex(k), sch.PropertySchemas(k.String()))
		if err != nil {
			return nil, err
		}
		if bv == nil {
			continue
		}
		elements[k.String()] = bv
	}

	return &BoundMapProperty{Schemas: sch, Elements: elements}, nil
}

func (b *propertyBinder) bindProperty(p reflect.Value, sch Schemas) (BoundNode, error) {
	if p.Kind() == reflect.Interface {
		p = p.Elem()
	}

	switch p.Kind() {
	case reflect.Bool:
		return &BoundLiteral{ExprType: TypeBool, Value: p.Bool()}, nil
	case reflect.Int:
		return &BoundLiteral{ExprType: TypeNumber, Value: p.Int()}, nil
	case reflect.Float64:
		return &BoundLiteral{ExprType: TypeNumber, Value: p.Float()}, nil
	case reflect.String:
		// attempt to parse the string as HIL. If the result is a simple literal, return that. Otherwise, keep the HIL
		// itself.
		rootNode, err := hil.Parse(p.String())
		if err != nil {
			return nil, err
		}
		contract.Assert(rootNode != nil)

		if lit, ok := rootNode.(*ast.LiteralNode); ok && lit.Typex == ast.TypeString {
			return &BoundLiteral{ExprType: TypeString, Value: lit.Value}, nil
		}

		return b.bindExpr(rootNode)
	case reflect.Slice:
		return b.bindListProperty(p, sch)
	case reflect.Map:
		return b.bindMapProperty(p, sch)
	default:
		return nil, errors.Errorf("unexpected property type %v", p.Type())
	}
}

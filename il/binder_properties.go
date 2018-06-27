package il

import (
	"github.com/hashicorp/hil/ast"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type propertyBinder struct {
	hil *hilBinder
}

func (b *propertyBinder) bindListProperty(s []interface{}, sch Schemas) (BoundNode, error) {
	if tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi) {
		switch len(s) {
		case 0:
			return nil, nil
		case 1:
			return b.bindProperty(s[0], sch.ElemSchemas())
		default:
			return nil, errors.Errorf("expected at most one item in list")
		}
	}

	elements := make([]BoundNode, 0, len(s))
	for _, v := range s {
		elem, err := b.bindProperty(v, sch.ElemSchemas())
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

func (b *propertyBinder) bindMapProperty(m map[string]interface{}, sch Schemas) (BoundNode, error) {
	elements := make(map[string]BoundNode)
	for k, v := range m {
		bv, err := b.bindProperty(v, sch.PropertySchemas(k))
		if err != nil {
			return nil, err
		}
		if bv == nil {
			continue
		}
		elements[k] = bv
	}

	return &BoundMapProperty{Schemas: sch, Elements: elements}, nil
}

func (b *propertyBinder) bindProperty(v interface{}, sch Schemas) (BoundNode, error) {
	switch v := v.(type) {
	case bool:
		return &BoundLiteral{ExprType: TypeBool, Value: v}, nil
	case float64:
		return &BoundLiteral{ExprType: TypeNumber, Value: v}, nil
	case string:
		return &BoundLiteral{ExprType: TypeString, Value: v}, nil
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


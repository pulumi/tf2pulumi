package il

import (
	"reflect"

	"github.com/hashicorp/hil"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"
)

// propertyBinder is used to convert Terraform configuration properties into a form better suited for static analysis
// and code generation. This process--referred to as binding--principally involves walking the AST for a property,
// associating IL nodes with variable references, attaching type information, and performing a few Pulumi-specific
// transforms.
type propertyBinder struct {
	builder       *builder
	hasCountIndex bool
}

// bindListProperty binds a list property according to the given schema information. If the schema information
// indicates that the list should be projected as its single element, the binder will return the bound element
// rather than the list itself. Note that this implies that the result of this function is not necessarily of
// type TypeList.
func (b *propertyBinder) bindListProperty(s reflect.Value, sch Schemas) (BoundNode, error) {
	contract.Require(s.Kind() == reflect.Slice, "s")

	// Grab the element schemas.
	elemSchemas := sch.ElemSchemas()

	// If this is a max-single-element list that we intend to project as its element, just bind its element and return.
	projectListElement := sch.TF != nil && sch.TF.Type == schema.TypeMap || tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi)
	if projectListElement {
		switch s.Len() {
		case 0:
			return nil, nil
		case 1:
			return b.bindProperty(s.Index(0), elemSchemas)
		default:
			return nil, errors.Errorf("expected at most one item in list")
		}
	}

	// Otherwise, bind each list element in turn according to the element schema.
	elements := make([]BoundNode, 0, s.Len())
	for i := 0; i < s.Len(); i++ {
		elem, err := b.bindProperty(s.Index(i), elemSchemas)
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

// bindMapProperty binds a map property according to the given schema.
func (b *propertyBinder) bindMapProperty(m reflect.Value, sch Schemas) (*BoundMapProperty, error) {
	contract.Require(m.Kind() == reflect.Map, "m")

	// Grab the key type and ensure it is of type string.
	if m.Type().Key().Kind() != reflect.String {
		return nil, errors.Errorf("unexpected key type %v", m.Type().Key())
	}

	// Bind each property in turn according to its appropriate schema.
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

// bindProperty binds a single Terraform property. This property must be of kind bool, int, float64, string, slice, or
// map. If this property is a map, its keys must be of kind string.
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
		// As in Terraform, parse all strings as HIL, then bind the result.
		rootNode, err := hil.Parse(p.String())
		if err != nil {
			return nil, err
		}
		contract.Assert(rootNode != nil)
		return b.bindExpr(rootNode)
	case reflect.Slice:
		return b.bindListProperty(p, sch)
	case reflect.Map:
		return b.bindMapProperty(p, sch)
	default:
		return nil, errors.Errorf("unexpected property type %v", p.Type())
	}
}

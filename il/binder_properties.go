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

package il

import (
	"fmt"
	"reflect"

	"github.com/hashicorp/hil"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
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
func (b *propertyBinder) bindListProperty(path string, s reflect.Value, sch Schemas) (BoundNode, error) {
	contract.Require(s.Kind() == reflect.Slice, "s")

	// Grab the element schemas.
	elemSchemas := sch.ElemSchemas()

	// If this is a max-single-element list that we intend to project as its element, just bind its element and return.
	var projectListElement bool
	if sch.TF != nil && sch.TF.Type == schema.TypeMap {
		elemSchemas, projectListElement = sch, true
	} else {
		projectListElement = tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi)
	}
	var err error
	if projectListElement {
		switch s.Len() {
		case 0:
			return nil, nil
		case 1:
			return b.bindProperty(path+"[0]", s.Index(0), elemSchemas)
		default:
			err = errors.Errorf("%v: expected at most one item in list, got %v", path, s.Len())
		}
	}

	// Otherwise, bind each list element in turn according to the element schema.
	elements := make([]BoundNode, 0, s.Len())
	for i := 0; i < s.Len(); i++ {
		elem, err := b.bindProperty(fmt.Sprintf("%s[%v]", path, i), s.Index(i), elemSchemas)
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

	boundList := &BoundListProperty{Schemas: sch, Elements: elements}
	if err != nil {
		return &BoundError{Value: boundList, NodeType: boundList.Type(), Error: err}, nil
	}
	return boundList, nil
}

// bindMapProperty binds a map property according to the given schema.
func (b *propertyBinder) bindMapProperty(path string, m reflect.Value, sch Schemas) (*BoundMapProperty, error) {
	contract.Require(m.Kind() == reflect.Map, "m")

	// Grab the key type and ensure it is of type string.
	if m.Type().Key().Kind() != reflect.String {
		return nil, errors.Errorf("%v: unexpected key type %v", path, m.Type().Key())
	}

	// Bind each property in turn according to its appropriate schema.
	elements := make(map[string]BoundNode)
	for _, k := range m.MapKeys() {
		bv, err := b.bindProperty(fmt.Sprintf("%v.%v", path, k), m.MapIndex(k), sch.PropertySchemas(k.String()))
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
func (b *propertyBinder) bindProperty(path string, p reflect.Value, sch Schemas) (BoundNode, error) {
	if p.Kind() == reflect.Interface {
		p = p.Elem()
	}

	switch p.Kind() {
	case reflect.Bool:
		return &BoundLiteral{ExprType: TypeBool, Value: p.Bool()}, nil
	case reflect.Int:
		return &BoundLiteral{ExprType: TypeNumber, Value: float64(p.Int())}, nil
	case reflect.Float64:
		return &BoundLiteral{ExprType: TypeNumber, Value: p.Float()}, nil
	case reflect.String:
		// As in Terraform, parse all strings as HIL, then bind the result.
		rootNode, err := hil.Parse(p.String())
		if err != nil {
			return nil, errors.Errorf("%v: could not parse HIL (%v)", path, err)
		}
		contract.Assert(rootNode != nil)
		n, err := b.bindExpr(rootNode)
		if err != nil {
			return nil, errors.Errorf("%v: %v", path, err)
		}
		return n, nil
	case reflect.Slice:
		return b.bindListProperty(path, p, sch)
	case reflect.Map:
		return b.bindMapProperty(path, p, sch)
	default:
		return nil, errors.Errorf("%v: unexpected property type %v", path, p.Type())
	}
}

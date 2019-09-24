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
	"strconv"
)

func coerceLiteral(lit *BoundLiteral, from, to Type) (*BoundLiteral, bool) {
	var str string
	switch from {
	case TypeBool:
		str = strconv.FormatBool(lit.Value.(bool))
	case TypeNumber:
		str = strconv.FormatFloat(lit.Value.(float64), 'g', -1, 64)
	case TypeString:
		str = lit.Value.(string)
	default:
		panic(fmt.Sprintf("unexpected literal type in coerceLiteral: %v", lit.Value))
	}

	switch to {
	case TypeBool:
		if str == "" {
			return &BoundLiteral{ExprType: TypeBool, Value: false}, true
		}
		val, err := strconv.ParseBool(str)
		if err == nil {
			return &BoundLiteral{ExprType: TypeBool, Value: val}, true
		}
	case TypeNumber:
		if str == "" {
			return &BoundLiteral{ExprType: TypeNumber, Value: 0.0}, true
		}
		val, err := strconv.ParseFloat(str, 64)
		if err == nil {
			return &BoundLiteral{ExprType: TypeNumber, Value: val}, true
		}
	case TypeString:
		return &BoundLiteral{ExprType: TypeString, Value: str}, true
	}

	return nil, false
}

func canMakeCoerceCall(from, to Type) bool {
	switch from {
	case TypeBool, TypeNumber:
		return to == TypeString
	case TypeString:
		return to == TypeBool || to == TypeNumber
	default:
		return false
	}
}

// makeCoercion inserts a call to the `__coerce` intrinsic if one is required to convert the given expression to the
// given type. If the input node is statically coercable according to the semantics of
// "github.com/hashicorp/terraform/helper/schema.stringToPrimitive".
func makeCoercion(n BoundNode, toType Type) BoundNode {
	// TODO: we really need dynamic coercions for the negative case.
	from, to := n.Type().ElementType(), toType.ElementType()

	e, ok := n.(BoundExpr)
	if !ok || from == to {
		return n
	}

	// If we're dealing with a literal, we can always try to convert through a string.
	if lit, ok := n.(*BoundLiteral); ok {
		if result, ok := coerceLiteral(lit, from, to); ok {
			return result
		}
	}

	// Otherwise, we will either do nothing (for conversions we don't support), or emit a call to the __coerce
	// intrinsic. That call will later be generated as an appropriate dynamic coercion.
	if !canMakeCoerceCall(from, to) {
		return n
	}
	return NewCoerceCall(e, toType)
}

// AddCoercions inserts calls to the `__coerce` intrinsic in cases where a list or map element's type disagrees with
// the element type present in the list or map's schema.
func AddCoercions(prop BoundNode) (BoundNode, error) {
	rewriter := func(n BoundNode) (BoundNode, error) {
		switch n := n.(type) {
		case *BoundListProperty:
			elemType := n.Schemas.ElemSchemas().Type()
			for i := range n.Elements {
				n.Elements[i] = makeCoercion(n.Elements[i], elemType)
			}
		case *BoundMapProperty:
			for k := range n.Elements {
				n.Elements[k] = makeCoercion(n.Elements[k], n.Schemas.PropertySchemas(k).Type())
			}
		}
		return n, nil
	}

	return VisitBoundNode(prop, IdentityVisitor, rewriter)
}

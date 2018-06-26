package nodejs

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/hashicorp/hil/ast"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/gen"
)

func coerceProperty(value string, valueType, propertyType ast.Type) string {
	// We only coerce values we know are strings.
	if valueType == propertyType || valueType != ast.TypeString {
		return value
	}

	switch propertyType {
	case ast.TypeBool:
		if value == "\"true\"" {
			return "true"
		} else if value == "\"false\"" {
			return "false"
		}
		return fmt.Sprintf("(%s === \"true\")", value)
	case ast.TypeInt, ast.TypeFloat:
		return fmt.Sprintf("Number.parseFloat(%s)", value)
	default:
		return value
	}
}

type propertyComputer struct {
	g *Generator
	countIndex string
}

func (pc *propertyComputer) computeHILProperty(n ast.Node) (string, ast.Type, error) {
	// NOTE: this will need to change in order to deal with combinations of resource outputs and other operators: most
	// translations will not be output-aware, so we'll need to transform things into applies.
	return (&hilWalker{pc: pc}).walkNode(n)
}

func (pc *propertyComputer) computeSliceProperty(s []interface{}, indent string, sch schemas) (string, ast.Type, error) {
	buf := &bytes.Buffer{}

	elemSch := sch.elemSchemas()
	if tfbridge.IsMaxItemsOne(sch.tf, sch.pulumi) {
		switch len(s) {
		case 0:
			return "undefined", ast.TypeUnknown, nil
		case 1:
			return pc.computeProperty(s[0], indent, elemSch)
		default:
			return "", ast.TypeInvalid, errors.Errorf("expected at most one item in list")
		}
	}

	fmt.Fprintf(buf, "[")
	for _, v := range s {
		elemIndent := indent + "    "
		elem, elemTyp, err := pc.computeProperty(v, elemIndent, elemSch)
		if err != nil {
			return "", ast.TypeInvalid, err
		}
		if elemTyp == ast.TypeList {
			// TF flattens list elements that are themselves lists into the parent list.
			//
			// TODO: if there is a list element that is dynamically a list, that also needs to be flattened. This is
			// only knowable at runtime and will require a helper.
			elem = "..." + elem
		}
		fmt.Fprintf(buf, "\n%s%s,", elemIndent, coerceProperty(elem, elemTyp, elemSch.astType()))
	}
	fmt.Fprintf(buf, "\n%s]", indent)
	return buf.String(), ast.TypeList, nil
}

func (pc *propertyComputer) computeMapProperty(m map[string]interface{}, indent string, sch schemas) (string, ast.Type, error) {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "{")
	for _, k := range gen.SortedKeys(m) {
		v := m[k]

		propSch := sch.propertySchemas(k)

		propIndent := indent + "    "
		prop, propTyp, err := pc.computeProperty(v, propIndent, propSch)
		if err != nil {
			return "", ast.TypeInvalid, err
		}
		prop = coerceProperty(prop, propTyp, propSch.astType())

		fmt.Fprintf(buf, "\n%s%s: %s,", propIndent, tsName(k, propSch.tf, propSch.pulumi, true), prop)
	}
	fmt.Fprintf(buf, "\n%s}", indent)
	return buf.String(), ast.TypeMap, nil
}

func (pc *propertyComputer) computeProperty(v interface{}, indent string, sch schemas) (string, ast.Type, error) {
	if node, ok := v.(ast.Node); ok {
		return pc.computeHILProperty(node)
	}

	refV := reflect.ValueOf(v)
	switch refV.Kind() {
	case reflect.Bool:
		return fmt.Sprintf("%v", v), ast.TypeBool, nil
	case reflect.Int, reflect.Float64:
		return fmt.Sprintf("%v", v), ast.TypeFloat, nil
	case reflect.String:
		return fmt.Sprintf("%q", v), ast.TypeString, nil
	case reflect.Slice:
		return pc.computeSliceProperty(v.([]interface{}), indent, sch)
	case reflect.Map:
		return pc.computeMapProperty(v.(map[string]interface{}), indent, sch)
	default:
		contract.Failf("unexpected property type %v", refV.Type())
		return "", ast.TypeInvalid, errors.New("unexpected property type")
	}
}


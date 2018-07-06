package il

import (
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

// Schemas bundles a property's Terraform and Pulumi schema information into a single type. This information is then
// used to determine type and name information for the property. If the Terraform property is of a composite type--a
// map, list, or set--the property's schemas may also be used to access child schemas.
type Schemas struct {
	TF     *schema.Schema
	TFRes  *schema.Resource
	Pulumi *tfbridge.SchemaInfo
}

// PropertySchemas returns the Schemas for the child property with the given name. This is only valid if the current
// Schemas describe a map property.
func (s Schemas) PropertySchemas(key string) Schemas {
	var propSch Schemas

	if s.TFRes != nil && s.TFRes.Schema != nil {
		propSch.TF = s.TFRes.Schema[key]
	}

	if propSch.TF != nil {
		if propResource, ok := propSch.TF.Elem.(*schema.Resource); ok {
			propSch.TFRes = propResource
		}
	}

	if s.Pulumi != nil && s.Pulumi.Fields != nil {
		propSch.Pulumi = s.Pulumi.Fields[key]
	}

	return propSch
}

// ElemSchemas returns the element Schemas for a list property.
func (s Schemas) ElemSchemas() Schemas {
	var elemSch Schemas

	if s.TF != nil {
		switch e := s.TF.Elem.(type) {
		case *schema.Schema:
			elemSch.TF = e
		case *schema.Resource:
			elemSch.TFRes = e
		}
	}

	if s.Pulumi != nil {
		elemSch.Pulumi = s.Pulumi.Elem
	}

	return elemSch
}

// Type returns the appropriate bound type for the property associated with these Schemas.
func (s Schemas) Type() Type {
	if s.TF != nil {
		switch s.TF.Type {
		case schema.TypeBool:
			return TypeBool
		case schema.TypeInt, schema.TypeFloat:
			return TypeNumber
		case schema.TypeString:
			return TypeString
		case schema.TypeList, schema.TypeSet:
			return s.ElemSchemas().Type().ListOf()
		case schema.TypeMap:
			return TypeMap
		default:
			return TypeUnknown
		}
	}

	return TypeUnknown
}

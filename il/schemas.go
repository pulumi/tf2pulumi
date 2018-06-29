package il

import (
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type Schemas struct {
	TF     *schema.Schema
	TFRes  *schema.Resource
	Pulumi *tfbridge.SchemaInfo
}

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
			// TODO: might need to do max-items-one projection here
			return s.ElemSchemas().Type().ListOf()
		case schema.TypeMap:
			return TypeMap
		default:
			return TypeUnknown
		}
	} else if s.TFRes != nil {
		return TypeMap
	}

	return TypeUnknown
}

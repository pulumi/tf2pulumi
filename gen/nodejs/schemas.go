package nodejs

import (
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type schemas struct {
	tf     *schema.Schema
	tfRes  *schema.Resource
	pulumi *tfbridge.SchemaInfo
}

func (s schemas) propertySchemas(key string) schemas {
	var propSch schemas

	if s.tfRes != nil && s.tfRes.Schema != nil {
		propSch.tf = s.tfRes.Schema[key]
	}

	if propSch.tf != nil {
		if propResource, ok := propSch.tf.Elem.(*schema.Resource); ok {
			propSch.tfRes = propResource
		}
	}

	if s.pulumi != nil && s.pulumi.Fields != nil {
		propSch.pulumi = s.pulumi.Fields[key]
	}

	return propSch
}

func (s schemas) elemSchemas() schemas {
	var elemSch schemas

	if s.tf != nil {
		switch e := s.tf.Elem.(type) {
		case *schema.Schema:
			elemSch.tf = e
		case *schema.Resource:
			elemSch.tfRes = e
		}
	}

	if s.pulumi != nil {
		elemSch.pulumi = s.pulumi.Elem
	}

	return elemSch
}

func (s schemas) astType() ast.Type {
	if s.tf != nil {
		switch s.tf.Type {
		case schema.TypeBool:
			return ast.TypeBool
		case schema.TypeInt, schema.TypeFloat:
			return ast.TypeFloat
		case schema.TypeString:
			return ast.TypeString
		case schema.TypeList, schema.TypeSet:
			// TODO: might need to do max-items-one projection here
			return ast.TypeList
		case schema.TypeMap:
			return ast.TypeMap
		default:
			return ast.TypeUnknown
		}
	} else if s.tfRes != nil {
		return ast.TypeMap
	}

	return ast.TypeUnknown
}


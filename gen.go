package main

import (
	"reflect"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/hil"
	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"

	_ "github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

type propertyWalker struct{}

func (propertyWalker) Map(v reflect.Value) error {
	// TODO: start map context
	return nil
}

func (propertyWalker) MapElem(m, k, v reflect.Value) error {
	// TODO: start element context
	return nil
}

func (propertyWalker) Slice(v reflect.Value) error {
	// TODO: start slice context
	return nil
}

func (propertyWalker) SliceElem(i int, v reflect.Value) error {
	// TODO: start slice element context
	return nil
}

func (propertyWalker) Primitive(v reflect.Value) error {
	if v.Kind() == reflect.Interface {
		v = v.Elem
	}
	if v.Kind() != reflect.String {
		return nil
	}

	rootNode, err := hil.Parse(v.String())
	if err != nil {
		return err
	}

	
}

func genProvider(p *config.ProviderConfig) error {
	spew.Dump(p)
	return nil
}

func genResource(r *config.Resource) error {
	spew.Dump(r)
	return nil
}

func genOutput(o *config.Output) error {
	spew.Dump(o)
	return nil
}

func genModule(m *module.Tree) error {
	if m == nil {
		return nil
	}

	conf := m.Config()

	for _, p := range conf.ProviderConfigs {
		if err := genProvider(p); err != nil {
			return err
		}
	}

	for _, r := range conf.Resources {
		if err := genResource(r); err != nil {
			return err
		}
	}

	for _, o := range conf.Outputs {
		if err := genOutput(o); err != nil {
			return err
		}
	}

	for _, c := range m.Children() {
		if err := genModule(c); err != nil {
			return err
		}
	}

	return nil
}

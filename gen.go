package main

import (
	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"

	_ "github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

func genProvider(p *config.ProviderConfig) error {
	spew.Dump(p)
	return nil
}

func genResource(r *config.Resource) error {
	spew.Dump(r)
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

	for _, c := range m.Children() {
		if err := genModule(c); err != nil {
			return err
		}
	}

	return nil
}

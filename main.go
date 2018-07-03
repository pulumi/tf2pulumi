package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform/command"
	"github.com/hashicorp/terraform/config/module"
	"github.com/hashicorp/terraform/svchost"
	"github.com/hashicorp/terraform/svchost/auth"
	"github.com/hashicorp/terraform/svchost/disco"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/gen/nodejs"
	"github.com/pgavlin/firewalker/il"
)

type noCredentials struct{}

func (noCredentials) ForHost(host svchost.Hostname) (auth.HostCredentials, error) {
	return nil, nil
}

func buildGraphs(tree *module.Tree, isRoot bool) ([]*il.Graph, error) {
	// TODO: move this into the il package and unify modules based on path

	children := []*il.Graph{}
	for _, c := range tree.Children() {
		cc, err := buildGraphs(c, false)
		if err != nil {
			return nil, err
		}
		children = append(children, cc...)
	}

	g, err := il.BuildGraph(tree)
	if err != nil {
		return nil, err
	}

	return append(children, g), nil
}

func main() {
	credentials := noCredentials{}
	services := disco.NewDisco()
	services.SetCredentialsSource(credentials)
	moduleStorage := module.NewStorage(filepath.Join(command.DefaultDataDir, "modules"), services, credentials)

	mod, err := module.NewTreeModule("", ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load module: %v\n", err)
		os.Exit(-1)
	}

	if err = mod.Load(moduleStorage); err != nil {
		fmt.Fprintf(os.Stderr, "could not load module: %v\n", err)
		os.Exit(-1)
	}

	log.Printf("loaded module: %v", mod)

	gs, err := buildGraphs(mod, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not build graphs: %v\n", err)
		os.Exit(-1)
	}

	if err = gen.Generate(gs, &nodejs.Generator{ProjectName: "auto"}); err != nil {
		fmt.Fprintf(os.Stderr, "generation failed: %v\n", err)
		os.Exit(-1)
	}
}

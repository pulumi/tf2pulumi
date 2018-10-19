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

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/gen/nodejs"
	"github.com/pulumi/tf2pulumi/il"
)

// The version of this tool.
var Version string

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
	services := disco.NewWithCredentialsSource(noCredentials{})
	moduleStorage := module.NewStorage(filepath.Join(command.DefaultDataDir, "modules"), services)

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
		fmt.Fprintf(os.Stderr, "could not import Terraform project: %v\n", err)
		os.Exit(-1)
	}

	if err = gen.Generate(gs, &nodejs.Generator{ProjectName: "auto"}); err != nil {
		fmt.Fprintf(os.Stderr, "generation failed: %v\n", err)
		os.Exit(-1)
	}
}

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

package convert

import (
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform/command"
	"github.com/hashicorp/terraform/config/module"
	"github.com/hashicorp/terraform/svchost"
	"github.com/hashicorp/terraform/svchost/auth"
	"github.com/hashicorp/terraform/svchost/disco"
	"github.com/pkg/errors"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/gen/nodejs"
	"github.com/pulumi/tf2pulumi/il"
)

// Convert converts a Terraform module at the provided location into a Pulumi module, written to stdout.
func Convert(opts Options) error {
	// Set default options where appropriate.
	if opts.Path == "" {
		opts.Path = "."
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	services := disco.NewWithCredentialsSource(noCredentials{})
	moduleStorage := module.NewStorage(filepath.Join(command.DefaultDataDir, "modules"), services)

	mod, err := module.NewTreeModule("", opts.Path)
	if err != nil {
		return errors.Wrapf(err, "creating tree module")
	}

	if err = mod.Load(moduleStorage); err != nil {
		return errors.Wrapf(err, "loading module")
	}

	gs, err := buildGraphs(mod, true, opts)
	if err != nil {
		return errors.Wrapf(err, "importing Terraform project graphs")
	}

	if err = gen.Generate(gs, nodejs.New("auto", opts.Writer)); err != nil {
		return errors.Wrapf(err, "generating code")
	}

	return nil
}

type Options struct {
	// AllowMissingPlugins, if true, code-gen continues even if resource provider plugins are missing.
	AllowMissingPlugins bool
	// Path, when set, overrides the default path (".") to load the source Terraform module from.
	Path string
	// Writer can be set to override the default behavior of writing the resulting code to stdout.
	Writer io.Writer
}

type noCredentials struct{}

func (noCredentials) ForHost(host svchost.Hostname) (auth.HostCredentials, error) {
	return nil, nil
}

func buildGraphs(tree *module.Tree, isRoot bool, opts Options) ([]*il.Graph, error) {
	// TODO: move this into the il package and unify modules based on path

	children := []*il.Graph{}
	for _, c := range tree.Children() {
		cc, err := buildGraphs(c, false, opts)
		if err != nil {
			return nil, err
		}
		children = append(children, cc...)
	}

	g, err := il.BuildGraph(tree, opts.AllowMissingPlugins)
	if err != nil {
		return nil, err
	}

	return append(children, g), nil
}

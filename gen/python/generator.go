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

// Package python implements a Python back-end for tf2pulumi's intermediate representation. It is responsible for
// translating the Graph IR emit by the frontend into valid Pulumi Python code that is as semantically equivalent to
// the original Terraform as possible.
package python

import (
	"errors"
	"io"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// New creates a new Python Generator that writes to the given writer and uses the given project name.
func New(projectName string, w io.Writer) gen.Generator {
	return &generator{
		projectName: projectName,
		w:           w,
	}
}

type generator struct {
	projectName string
	w           io.Writer
}

func (g *generator) GeneratePreamble(gs []*il.Graph) error {
	return nil
}

func (g *generator) BeginModule(mod *il.Graph) error {
	if len(mod.Tree.Path()) != 0 {
		return errors.New("NYI: Python Modules")
	}
	return nil
}

func (g *generator) EndModule(mod *il.Graph) error {
	return nil
}

func (g *generator) GenerateVariables(vs []*il.VariableNode) error {
	if len(vs) != 0 {
		return errors.New("NYI: Python Variables")
	}
	return nil
}

func (g *generator) GenerateModule(m *il.ModuleNode) error {
	return errors.New("NYI: Python Modules")
}

func (g *generator) GenerateLocal(l *il.LocalNode) error {
	return errors.New("NYI: Python Locals")
}

func (g *generator) GenerateResource(r *il.ResourceNode) error {
	return errors.New("NYI: Python Resources")
}

func (g *generator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) != 0 {
		return errors.New("NYI: Python Outputs")
	}
	return nil
}

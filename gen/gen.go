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

package gen

import (
	"github.com/pkg/errors"

	"github.com/pulumi/tf2pulumi/il"
)

// Generator defines the interface that a language-specific code generator must expose in order to generate code for a
// set of Terraform modules.
type Generator interface {
	// GeneratePreamble generates any preamble required by the language. The complete list of modules that will be
	// generated is passed as an input.
	GeneratePreamble(gs []*il.Graph) error
	// BeginModule does any work specific to the generation of a new module definition.
	BeginModule(g *il.Graph) error
	// EndModule does any work specific to the end of code generation for a module definition.
	EndModule(g *il.Graph) error
	// GenerateProvider generates a single provider block in the context of the current module definition.
	GenerateProvider(p *il.ProviderNode) error
	// GenerateVariables generates variable definitions in the context of the current module definition.
	GenerateVariables(vs []*il.VariableNode) error
	// GenerateModule generates a single module instantiation in the context of the current module definition.
	GenerateModule(m *il.ModuleNode) error
	// GenerateLocal generates a single local value definition in the context of the current module definition.
	GenerateLocal(l *il.LocalNode) error
	// GenerateResource generates a single resource instantiation in the context of the current module definition.
	GenerateResource(r *il.ResourceNode) error
	// GenerateOutputs generates the list of outputs in the context of the current module definition.
	GenerateOutputs(os []*il.OutputNode) error
}

// generateNode generates a single local value, module, or resource node, ensuring that its dependencies have been
// generated before it is itself generated.
func generateNode(n il.Node, lang Generator, done map[il.Node]struct{}) error {
	return generateDependency(n, lang, map[il.Node]struct{}{}, done)
}

func generateDependency(n il.Node, lang Generator, inProgress, done map[il.Node]struct{}) error {
	if _, ok := done[n]; ok {
		return nil
	}
	if _, ok := inProgress[n]; ok {
		return errors.Errorf("circular dependency detected")
	}
	inProgress[n] = struct{}{}

	for _, d := range n.Dependencies() {
		if err := generateDependency(d, lang, inProgress, done); err != nil {
			return err
		}
	}

	var err error
	switch n := n.(type) {
	case *il.LocalNode:
		err = lang.GenerateLocal(n)
	case *il.ModuleNode:
		err = lang.GenerateModule(n)
	case *il.ProviderNode:
		err = lang.GenerateProvider(n)
	case *il.ResourceNode:
		err = lang.GenerateResource(n)
	default:
		return errors.Errorf("unexpected node type %T", n)
	}
	if err != nil {
		return err
	}

	done[n] = struct{}{}
	return nil
}

// generateModuleDef sequences the generation of a single module definition.
func generateModuleDef(g *il.Graph, lang Generator) error {
	// We currently do not support multiple provider instantiations, so fail if any providers have dependencies on
	// nodes that do not represent config vars.
	for _, p := range g.Providers {
		for _, d := range p.Deps {
			if _, ok := d.(*il.VariableNode); !ok {
				return errors.Errorf("unsupported provider dependency: %v", d)
			}
		}
	}

	if err := lang.BeginModule(g); err != nil {
		return err
	}

	// Variables are sources. Generate them first.
	vars := make([]*il.VariableNode, len(g.Variables))
	for i, k := range SortedKeys(g.Variables) {
		vars[i] = g.Variables[k]
	}
	if err := lang.GenerateVariables(vars); err != nil {
		return err
	}

	// Next, collect all resources and locals and generate them in topological order.
	done := make(map[il.Node]struct{})
	for _, v := range g.Variables {
		done[v] = struct{}{}
	}
	todo := make([]il.Node, 0)

	localKeys := SortedKeys(g.Locals)
	moduleKeys := SortedKeys(g.Modules)
	providerKeys := SortedKeys(g.Providers)
	resourceKeys := SortedKeys(g.Resources)
	for _, k := range localKeys {
		l := g.Locals[k]
		if len(l.Deps) == 0 {
			if err := generateNode(l, lang, done); err != nil {
				return err
			}
		} else {
			todo = append(todo, l)
		}
	}
	for _, k := range moduleKeys {
		m := g.Modules[k]
		if len(m.Deps) == 0 {
			if err := generateNode(m, lang, done); err != nil {
				return err
			}
		} else {
			todo = append(todo, m)
		}
	}
	for _, k := range providerKeys {
		p := g.Providers[k]
		if len(p.Deps) == 0 {
			if err := generateNode(p, lang, done); err != nil {
				return err
			}
		} else {
			todo = append(todo, p)
		}
	}
	for _, k := range resourceKeys {
		r := g.Resources[k]
		if len(r.Deps) == 0 {
			if err := generateNode(r, lang, done); err != nil {
				return err
			}
		} else {
			todo = append(todo, r)
		}
	}
	for _, n := range todo {
		if err := generateNode(n, lang, done); err != nil {
			return err
		}
	}

	// Finally, generate all outputs. These are sinks, so all of their dependencies should already have been generated.
	outputs := make([]*il.OutputNode, len(g.Outputs))
	for i, k := range SortedKeys(g.Outputs) {
		outputs[i] = g.Outputs[k]
	}
	for _, o := range outputs {
		for _, d := range o.Deps {
			if _, ok := done[d]; !ok {
				return errors.Errorf("output has unsatisfied dependency %v", d)
			}
		}
	}
	if err := lang.GenerateOutputs(outputs); err != nil {
		return err
	}

	return lang.EndModule(g)
}

// Generate generates source for a list of modules using the given language-specific generator.
func Generate(modules []*il.Graph, lang Generator) error {
	// Generate any necessary preamble.
	if err := lang.GeneratePreamble(modules); err != nil {
		return err
	}

	// Generate modules.
	for _, g := range modules {
		if err := generateModuleDef(g, lang); err != nil {
			return err
		}
	}

	return nil
}

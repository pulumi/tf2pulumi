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
	"sort"

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

// sortNodesBySourceOrder sorts the given slice of nodes by file, then line, then column, then node ID.
func sortNodesBySourceOrder(n []il.Node) []il.Node {
	sort.Slice(n, func(i, j int) bool {
		return lessInSourceOrder(n[i], n[j])
	})
	return n
}

// lessInSourceOrder returns true if a precedes b when ordered first by filename, then by line,
// then by column, and finally by node ID.
func lessInSourceOrder(a, b il.Node) bool {
	al, bl := a.GetLocation(), b.GetLocation()
	if al.Filename < bl.Filename {
		return true
	}
	if al.Filename > bl.Filename {
		return false
	}
	if al.Line < bl.Line {
		return true
	}
	if al.Line > bl.Line {
		return false
	}
	if al.Column < bl.Column {
		return true
	}
	if al.Column > bl.Column {
		return false
	}
	return a.ID() < b.ID()
}

// generateNode generates a single local value, module, or resource node, ensuring that its dependencies have been
// generated before it is itself generated.
func generateNode(n il.Node, lang Generator, done map[il.Node]bool) error {
	return generateDependency(n, lang, map[il.Node]bool{}, done)
}

func generateDependency(n il.Node, lang Generator, inProgress, done map[il.Node]bool) error {
	if _, ok := done[n]; ok {
		return nil
	}
	if _, ok := inProgress[n]; ok {
		return errors.Errorf("circular dependency detected")
	}
	inProgress[n] = true

	for _, d := range sortNodesBySourceOrder(n.Dependencies()) {
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
	case *il.VariableNode:
		// Nothing to do; these have already been generated.
	default:
		return errors.Errorf("unexpected node type %T", n)
	}
	if err != nil {
		return err
	}

	done[n] = true
	return nil
}

// generateInnerNodes generates all locals and module, provider, and resource instantiations in a graph. Variables
// must have been generated prior to calling this function, and outputs should be generated afterwards. A node's
// dependencies are guaranteed to be generated before the node itself (i.e. nodes are generated in a valid topological
// order).
//
// This function goes to some length to ensure that definitions are grouped by source file to the greatest possible
// extent. It does so by picking the file to generate that has the fewest references to nodes that have not yet been
// generated and are defined in other files and iterating until all files have been generated. Inside a file, nodes
// are generated in order by their appearance in their original source file. Any nodes that are out-of-order must be
// out-of-order to satisfy the requirement that nodes are generated in a valid topological order.
func generateInnerNodes(g *il.Graph, lang Generator) error {
	type file struct {
		name  string    // The name of the Terraform source file.
		nodes []il.Node // The list of nodes defined by the source file.
	}

	// First, collect nodes into files. Ignore config and outputs, as these are sources and sinks, respectively.
	files := map[string]*file{}
	addNode := func(n il.Node) {
		filename := n.GetLocation().Filename
		f, ok := files[filename]
		if !ok {
			f = &file{name: filename}
			files[filename] = f
		}
		f.nodes = append(f.nodes, n)
	}
	for _, n := range g.Modules {
		addNode(n)
	}
	for _, n := range g.Providers {
		addNode(n)
	}
	for _, n := range g.Resources {
		addNode(n)
	}
	for _, n := range g.Locals {
		addNode(n)
	}

	// Now build a worklist out of the set of files, sorting the nodes in each file in source order as we go.
	worklist := make([]*file, 0, len(files))
	for _, f := range files {
		sortNodesBySourceOrder(f.nodes)
		worklist = append(worklist, f)
	}

	// While the worklist is not empty, generate the nodes in the file with the fewest unsatisfied dependencies on
	// nodes in other files.
	doneNodes := map[il.Node]bool{}
	for len(worklist) > 0 {
		// Recalculate file weights and find the file with the lowest weight.
		var next *file
		var nextIndex, nextWeight int
		for i, f := range worklist {
			weight, processed := 0, map[il.Node]bool{}
			for _, n := range f.nodes {
				for _, d := range n.Dependencies() {
					// We don't count variable nodes (they've always been generated prior to calling this function),
					// nodes that we've already counted, or nodes that have already been generated.
					if _, isVar := d.(*il.VariableNode); isVar || processed[d] || doneNodes[d] {
						continue
					}

					// If this dependency resides in a different file, increment the current file's weight and mark the
					// depdendency as processed.
					depFilename := d.GetLocation().Filename
					if depFilename != f.name {
						weight++
					}
					processed[d] = true
				}
			}

			// If we haven't yet chosen a file to generate or if this file has fewer unsatisfied dependencies than the
			// current choice, choose this file. Ties are broken by the lexical order of the filenames.
			if next == nil || weight < nextWeight || weight == nextWeight && f.name < next.name {
				next, nextIndex, nextWeight = f, i, weight
			}
		}

		// Swap the chosen file with the tail of the list, then trim the worklist by one.
		worklist[len(worklist)-1], worklist[nextIndex] = worklist[nextIndex], worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]

		// Now generate the nodes in the chosen file and mark the file as done.
		for _, n := range next.nodes {
			if err := generateNode(n, lang, doneNodes); err != nil {
				return err
			}
		}
	}
	return nil
}

// generateModuleDef sequences the generation of a single module definition.
func generateModuleDef(g *il.Graph, lang Generator) error {
	if err := lang.BeginModule(g); err != nil {
		return err
	}

	// Variables are sources. Generate them first.
	vars := make([]*il.VariableNode, 0, len(g.Variables))
	for _, v := range g.Variables {
		vars = append(vars, v)
	}
	sort.Slice(vars, func(i, j int) bool { return lessInSourceOrder(vars[i], vars[j]) })
	if err := lang.GenerateVariables(vars); err != nil {
		return err
	}

	// Next, generate all resources, locals, and providers in topological order.
	if err := generateInnerNodes(g, lang); err != nil {
		return err
	}

	// Finally, generate all outputs. These are sinks, so all of their dependencies should already have been generated.
	outputs := make([]*il.OutputNode, 0, len(g.Outputs))
	for _, o := range g.Outputs {
		outputs = append(outputs, o)
	}
	sort.Slice(outputs, func(i, j int) bool { return lessInSourceOrder(outputs[i], outputs[j]) })
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

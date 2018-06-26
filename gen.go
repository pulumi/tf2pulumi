package main

import (
	"github.com/pkg/errors"
)

type generator interface {
	generatePreamble(g *graph) error
	generateVariables(vs []*variableNode) error
	generateLocal(l *localNode) error
	generateResource(r *resourceNode) error
	generateOutputs(os []*outputNode) error
}

func generateNode(n node, lang generator, done map[node]struct{}) error {
	if _, ok := done[n]; ok {
		return nil
	}

	for _, d := range n.dependencies() {
		if err := generateNode(d, lang, done); err != nil {
			return err
		}
	}

	var err error
	switch n := n.(type) {
	case *localNode:
		err = lang.generateLocal(n)
	case *resourceNode:
		err = lang.generateResource(n)
	default:
		return errors.Errorf("unexpected node type %T", n)
	}
	if err != nil {
		return err
	}

	done[n] = struct{}{}
	return nil
}

func generate(g *graph, lang generator) error {
	// We currently do not support multiple provider instantiations, so fail if any providers have dependencies on
	// nodes that do not represent config vars.
	for _, p := range g.providers {
		for _, d := range p.deps {
			if _, ok := d.(*variableNode); !ok {
				return errors.Errorf("unsupported provider dependency: %v", d)
			}
		}
	}

	// Generate any necessary preamble.
	if err := lang.generatePreamble(g); err != nil {
		return err
	}

	// Variables are sources. Generate them first.
	vars := make([]*variableNode, len(g.variables))
	for i, k := range sortedKeys(g.variables) {
		vars[i] = g.variables[k]
	}
	if err := lang.generateVariables(vars); err != nil {
		return err
	}

	// Next, collect all resources and locals and generate them in topological order.
	done := make(map[node]struct{})
	for _, v := range g.variables {
		done[v] = struct{}{}
	}
	todo := make([]node, 0)

	localKeys, resourceKeys := sortedKeys(g.locals), sortedKeys(g.resources)
	for _, k := range localKeys {
		l := g.locals[k]
		if len(l.deps) == 0 {
			if err := generateNode(l, lang, done); err != nil {
				return err
			}
		} else {
			todo = append(todo, l)
		}
	}
	for _, k := range resourceKeys {
		r := g.resources[k]
		if len(r.deps) == 0 {
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
	outputs := make([]*outputNode, len(g.outputs))
	for i, k := range sortedKeys(g.outputs) {
		outputs[i] = g.outputs[k]
	}
	for _, o := range outputs {
		for _, d := range o.deps {
			if _, ok := done[d]; !ok {
				return errors.Errorf("output has unsatisfied dependency %v", d)
			}
		}
	}
	if err := lang.generateOutputs(outputs); err != nil {
		return err
	}

	return nil
}

package main

import (
	"github.com/pkg/errors"

	"github.com/pgavlin/firewalker/il"
)

type generator interface {
	generatePreamble(g *il.Graph) error
	generateVariables(vs []*il.VariableNode) error
	generateLocal(l *il.LocalNode) error
	generateResource(r *il.ResourceNode) error
	generateOutputs(os []*il.OutputNode) error
}

func generateNode(n il.Node, lang generator, done map[il.Node]struct{}) error {
	if _, ok := done[n]; ok {
		return nil
	}

	for _, d := range n.Dependencies() {
		if err := generateNode(d, lang, done); err != nil {
			return err
		}
	}

	var err error
	switch n := n.(type) {
	case *il.LocalNode:
		err = lang.generateLocal(n)
	case *il.ResourceNode:
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

func generate(g *il.Graph, lang generator) error {
	// We currently do not support multiple provider instantiations, so fail if any providers have dependencies on
	// nodes that do not represent config vars.
	for _, p := range g.Providers {
		for _, d := range p.Deps {
			if _, ok := d.(*il.VariableNode); !ok {
				return errors.Errorf("unsupported provider dependency: %v", d)
			}
		}
	}

	// Generate any necessary preamble.
	if err := lang.generatePreamble(g); err != nil {
		return err
	}

	// Variables are sources. Generate them first.
	vars := make([]*il.VariableNode, len(g.Variables))
	for i, k := range sortedKeys(g.Variables) {
		vars[i] = g.Variables[k]
	}
	if err := lang.generateVariables(vars); err != nil {
		return err
	}

	// Next, collect all resources and locals and generate them in topological order.
	done := make(map[il.Node]struct{})
	for _, v := range g.Variables {
		done[v] = struct{}{}
	}
	todo := make([]il.Node, 0)

	localKeys, resourceKeys := sortedKeys(g.Locals), sortedKeys(g.Resources)
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
	for i, k := range sortedKeys(g.Outputs) {
		outputs[i] = g.Outputs[k]
	}
	for _, o := range outputs {
		for _, d := range o.Deps {
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

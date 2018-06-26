package gen

import (
	"github.com/pkg/errors"

	"github.com/pgavlin/firewalker/il"
)

type Generator interface {
	GeneratePreamble(g *il.Graph) error
	GenerateVariables(vs []*il.VariableNode) error
	GenerateLocal(l *il.LocalNode) error
	GenerateResource(r *il.ResourceNode) error
	GenerateOutputs(os []*il.OutputNode) error
}



func generateNode(n il.Node, lang Generator, done map[il.Node]struct{}) error {
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
		err = lang.GenerateLocal(n)
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

func Generate(g *il.Graph, lang Generator) error {
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
	if err := lang.GeneratePreamble(g); err != nil {
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

	localKeys, resourceKeys := SortedKeys(g.Locals), SortedKeys(g.Resources)
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

	return nil
}

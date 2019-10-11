package gen

import (
	"testing"

	"github.com/pulumi/tf2pulumi/internal/config"
	"github.com/pulumi/tf2pulumi/internal/config/module"
	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/il"
)

type testGen struct {
	t             *testing.T
	currentModule *il.Graph
	modules       map[*il.Graph][]il.Node
}

func (tg *testGen) GeneratePreamble(gs []*il.Graph) error {
	assert.Nil(tg.t, tg.modules)

	// Add entries to the module map for each graph.
	tg.modules = map[*il.Graph][]il.Node{}
	for _, g := range gs {
		tg.modules[g] = nil
	}
	return nil
}

func (tg *testGen) BeginModule(g *il.Graph) error {
	_, hasModule := tg.modules[g]
	assert.True(tg.t, hasModule)
	tg.currentModule = g
	return nil
}

func (tg *testGen) EndModule(g *il.Graph) error {
	assert.Equal(tg.t, tg.currentModule, g)
	return nil
}

func (tg *testGen) GenerateProvider(p *il.ProviderNode) error {
	tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], p)
	return nil
}

func (tg *testGen) GenerateVariables(vs []*il.VariableNode) error {
	for _, v := range vs {
		tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], v)
	}
	return nil
}

func (tg *testGen) GenerateModule(m *il.ModuleNode) error {
	tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], m)
	return nil
}

func (tg *testGen) GenerateLocal(l *il.LocalNode) error {
	tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], l)
	return nil
}

func (tg *testGen) GenerateResource(r *il.ResourceNode) error {
	tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], r)
	return nil
}

func (tg *testGen) GenerateOutputs(os []*il.OutputNode) error {
	for _, o := range os {
		tg.modules[tg.currentModule] = append(tg.modules[tg.currentModule], o)
	}
	return nil
}

func loadConfig(t *testing.T, path string) *config.Config {
	conf, err := config.LoadDir(path)
	if err != nil {
		t.Fatalf("could not load config at %s: %v", path, err)
	}
	return conf
}

func TestGenOrder(t *testing.T) {
	conf := loadConfig(t, "testdata/test_gen_order")
	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	lang := &testGen{t: t}
	err = Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(lang.modules))

	mainNodes, ok := lang.modules[g]
	assert.True(t, ok)
	actualIDs := make([]string, len(mainNodes))
	for i, n := range mainNodes {
		actualIDs[i] = n.ID()
	}

	expectedNodes := []il.Node{
		// Variables come first. All of these should be in source order.
		g.Variables["vpc_id"],
		g.Variables["availability_zone"],
		g.Variables["region_numbers"],
		g.Variables["az_numbers"],

		// Inner nodes are next, in "best" order. First is the implicitly-configured AWS provider.
		g.Providers["aws"],

		// Next are the nodes from variables.tf, as they have no dependencies on nodes defined in other files. These
		// nodes should be generated in source order.
		g.Resources["data.aws_availability_zone.target"],
		g.Resources["data.aws_vpc.target"],

		// Next come the nodes from subnet.tf, because they depend on the nodes in variables.tf but on no other nodes
		// defined in other files. The subnet and the route table are in source order, but the route table associtaion
		// is out-of-order as it depends on the other two resources.
		g.Resources["aws_subnet.main"],
		g.Resources["aws_route_table.main"],
		g.Resources["aws_route_table_association.main"],

		// Next comes the single node from security_group.tf, as it depends on resources from both variables.tf and
		// subnet.tf.
		g.Resources["aws_security_group.az"],

		// The outputs come last in source order.
		g.Outputs["subnet_id"],
		g.Outputs["security_group_id"],
	}
	expectedIDs := make([]string, len(expectedNodes))
	for i, n := range expectedNodes {
		expectedIDs[i] = n.ID()
	}

	assert.Equal(t, expectedIDs, actualIDs)
}

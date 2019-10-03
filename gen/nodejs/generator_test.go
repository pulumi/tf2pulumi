package nodejs

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

func TestLegalIdentifiers(t *testing.T) {
	legalIdentifiers := []string{
		"foobar",
		"$foobar",
		"_foobar",
		"_foo$bar",
		"_foo1bar",
		"Foobar",
	}
	for _, id := range legalIdentifiers {
		assert.True(t, isLegalIdentifier(id))
		assert.Equal(t, id, cleanName(id))
	}

	type illegalCase struct {
		original string
		expected string
	}
	illegalCases := []illegalCase{
		{"123foo", "_123foo"},
		{"foo.bar", "foo_bar"},
		{"$foo/bar", "$foo_bar"},
		{"12/bar\\baz", "_12_bar_baz"},
		{"foo bar", "foo_bar"},
		{"foo-bar", "foo_bar"},
		{".bar", "_bar"},
		{"1.bar", "_1_bar"},
	}
	for _, c := range illegalCases {
		assert.False(t, isLegalIdentifier(c.original))
		assert.Equal(t, c.expected, cleanName(c.original))
	}
}

func TestLowerToLiteral(t *testing.T) {
	prop := &il.BoundMapProperty{
		Elements: map[string]il.BoundNode{
			"key": &il.BoundOutput{
				Exprs: []il.BoundExpr{
					&il.BoundLiteral{
						ExprType: il.TypeString,
						Value:    "module: ",
					},
					&il.BoundVariableAccess{
						ExprType: il.TypeString,
						TFVar:    &config.PathVariable{Type: config.PathValueModule},
					},
					&il.BoundLiteral{
						ExprType: il.TypeString,
						Value:    " root: ",
					},
					&il.BoundVariableAccess{
						ExprType: il.TypeString,
						TFVar:    &config.PathVariable{Type: config.PathValueRoot},
					},
				},
			},
		},
	}

	g := &generator{
		rootPath: ".",
		module:   &il.Graph{Tree: module.NewTree("foo", &config.Config{Dir: "./foo/bar"})},
	}
	g.Emitter = gen.NewEmitter(nil, g)

	p, err := g.lowerToLiterals(prop)
	assert.NoError(t, err)

	pmap := p.(*il.BoundMapProperty)
	pout := pmap.Elements["key"].(*il.BoundOutput)

	lit1, ok := pout.Exprs[1].(*il.BoundLiteral)
	assert.True(t, ok)
	assert.Equal(t, "foo/bar", lit1.Value)

	lit3, ok := pout.Exprs[3].(*il.BoundLiteral)
	assert.True(t, ok)
	assert.Equal(t, ".", lit3.Value)

	computed, _, err := g.computeProperty(prop, false, "")
	assert.NoError(t, err)
	assert.Equal(t, "{\n    key: `module: foo/bar root: .`,\n}", computed)
}

func loadConfig(t *testing.T, path string) *config.Config {
	conf, err := config.LoadDir(path)
	if err != nil {
		t.Fatalf("could not load config at %s: %v", path, err)
	}
	return conf
}

func readFile(t *testing.T, path string) string {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read file %s: %v", path, err)
	}
	return string(bytes)
}

func TestComments(t *testing.T) {
	conf := loadConfig(t, "testdata/test_comments")

	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	var b bytes.Buffer
	lang, err := New("main", "0.16.0", false, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText16 := readFile(t, "testdata/test_comments/index.16.ts")
	assert.Equal(t, expectedText16, b.String())

	g, err = il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	b.Reset()
	lang, err = New("main", "0.17.1", false, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText17 := readFile(t, "testdata/test_comments/index.17.ts")
	assert.Equal(t, expectedText17, b.String())

	g, err = il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	b.Reset()
	lang, err = New("main", "0.17.28", true, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText17PromptDataSources := readFile(t, "testdata/test_comments/index.v1.ts")
	assert.Equal(t, expectedText17PromptDataSources, b.String())
}

func TestOrdering(t *testing.T) {
	conf := loadConfig(t, "testdata/test_ordering")
	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	var b bytes.Buffer
	lang, err := New("main", "1.0.0", true, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText := readFile(t, "testdata/test_ordering/index.ts")
	assert.Equal(t, expectedText, b.String())
}

func TestConditionals(t *testing.T) {
	conf := loadConfig(t, "testdata/test_conditionals")
	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	var b bytes.Buffer
	lang, err := New("main", "1.0.0", true, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText := readFile(t, "testdata/test_conditionals/index.ts")
	assert.Equal(t, expectedText, b.String())
}

func TestMetaProperties(t *testing.T) {
	conf := loadConfig(t, "testdata/test_meta_properties")
	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	var b bytes.Buffer
	lang, err := New("main", "1.0.0", true, &b)
	assert.NoError(t, err)
	err = gen.Generate([]*il.Graph{g}, lang)
	assert.NoError(t, err)

	expectedText := readFile(t, "testdata/test_meta_properties/index.ts")
	assert.Equal(t, expectedText, b.String())
}

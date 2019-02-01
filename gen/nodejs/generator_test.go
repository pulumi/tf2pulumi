package nodejs

import (
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/stretchr/testify/assert"

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
				HILNode: nil,
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

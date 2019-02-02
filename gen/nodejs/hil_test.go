package nodejs

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringLiteral(t *testing.T) {
	type literalCase struct {
		input    string
		expected string
	}

	cases := []literalCase{
		{"foobar", `"foobar"`},
		{`foo"bar`, `"foo\"bar"`},
		{`foo\bar`, `"foo\\bar"`},
		{"foobar\n", `"foobar\n"`},
		{"\nfoobar", `"\nfoobar"`},
		{"\nfoobar\n", "`\nfoobar\n`"},
		{"foo\nbar", "`foo\nbar`"},
		{"foo\nbar$", "`foo\nbar$`"},
		{"foo\nbar`", "`foo\nbar\\``"},
		{"foo\nbar\\", "`foo\nbar\\\\`"},
		{"foo\nbar${", "`foo\nbar\\${`"},
	}

	g := &generator{}
	for _, c := range cases {
		var b bytes.Buffer
		g.genStringLiteral(&b, c.input)
		assert.Equal(t, c.expected, b.String())
	}
}

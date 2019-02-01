package nodejs

import (
	"bytes"
	"testing"

	"github.com/pulumi/tf2pulumi/il"
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

func TestNumberParseFloat(t *testing.T) {
	type testCase struct {
		input    string
		expected string
	}

	// All of these should parse successfully.
	cases := []testCase{
		{"123a", `123`},
		{"+1.2", `1.2`},
		{"42", `42`},
		{"-42", `-42`},
		{"3.14e0", `3.14`},
		{"3.14E0", `3.14`},
		{"3.", `3`},
		{".3", `0.3`},
		{"3.e0", `3`},
		{".3e0", `0.3`},
		{".314e+1", `3.14`},
		{"31.4e-1", `3.14`},
	}

	g, b := &generator{}, &bytes.Buffer{}
	for _, c := range cases {
		b.Truncate(0)
		g.genCoercion(b, &il.BoundLiteral{ExprType: il.TypeString, Value: c.input}, il.TypeNumber)
		assert.Equal(t, c.expected, b.String())

		b.Truncate(0)
		g.genCoercion(b, &il.BoundLiteral{ExprType: il.TypeString, Value: c.input + "foo"}, il.TypeNumber)
		assert.Equal(t, c.expected, b.String())
	}

	negativeCases := []string{
		"abcd",
		"+abcd",
		"-abcd",
		".abcd",
		".e",
		".E",
	}
	for _, c := range negativeCases {
		b.Truncate(0)
		g.genCoercion(b, &il.BoundLiteral{ExprType: il.TypeString, Value: c}, il.TypeNumber)
		assert.Equal(t, `NaN`, b.String())
	}
}

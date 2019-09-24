package il

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringCoercions(t *testing.T) {
	type testCase struct {
		input    string
		expected interface{}
	}

	// All of these should parse successfully.
	cases := []testCase{
		{"", 0.0},
		{"123", 123.0},
		{"+1.2", 1.2},
		{"42", 42.0},
		{"-42", -42.0},
		{"3.14e0", 3.14},
		{"3.14E0", 3.14},
		{"3.", 3.0},
		{".3", 0.3},
		{"3.e0", 3.0},
		{".3e0", 0.3},
		{".314e+1", 3.14},
		{"31.4e-1", 3.14},
		{"", false},
		{"true", true},
		{"TRUE", true},
		{"false", false},
		{"FALSE", false},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			toType := TypeBool
			if _, ok := c.expected.(float64); ok {
				toType = TypeNumber
			}

			result := makeCoercion(&BoundLiteral{ExprType: TypeString, Value: c.input}, toType)
			lit, ok := result.(*BoundLiteral)
			assert.True(t, ok)
			assert.Equal(t, c.expected, lit.Value)
		})
	}

	type negativeCase struct {
		input  string
		toType Type
	}

	negativeCases := []negativeCase{
		{"abcd", TypeNumber},
		{"flase", TypeBool},
	}
	for _, c := range negativeCases {
		result := makeCoercion(&BoundLiteral{ExprType: TypeString, Value: c.input}, c.toType)
		_, ok := result.(*BoundCall)
		assert.True(t, ok)
	}
}

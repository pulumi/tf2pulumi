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

package il

import (
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/internal/config"
)

// Type represents the type of a single node in a bound property tree. Types are fairly simplistic: in addition to the
// primitive types--bool, string, number, map, and unknown--there are the composite types list and output. A type that
// is both a list and an output is considered to be an output of a list. Outputs have the semantic that their values may
// not be known promptly; in particular, the target language may need to introduce special elements (e.g. `apply`) to
// access the concrete value of an output.
type Type uint32

const (
	// TypeInvalid is self-explanatory.
	TypeInvalid Type = 0
	// TypeBool represents the universe of boolean values.
	TypeBool Type = 1
	// TypeString represents the universe of string values.
	TypeString Type = 1 << 1
	// TypeNumber represents the universe of real number values.
	TypeNumber Type = 1 << 2
	// TypeMap represents the universe of string -> unknown values.
	TypeMap Type = 1 << 3
	// TypeUnknown represnets the universe of unknown values. These values may have any type at runtime, and dynamic
	// conversions may be necessary when assigning these values to differently-typed destinations.
	TypeUnknown Type = 1 << 4

	// TypeList represents the universe of list values. A list's element type must be a primitive type.
	TypeList Type = 1 << 5
	// TypeOutput represents the universe of output value.
	TypeOutput Type = 1 << 6

	elementTypeMask Type = TypeBool | TypeString | TypeNumber | TypeMap | TypeUnknown
)

// IsList returns true if this value represents a list type.
func (t Type) IsList() bool {
	return t&TypeList != 0
}

// ListOf returns this a list type with this value as its element type.
func (t Type) ListOf() Type {
	return t | TypeList
}

// IsOutput returns true if this value represents an output type.
func (t Type) IsOutput() bool {
	return t&TypeOutput != 0
}

// ListOf returns this an output type with this value as its element type.
func (t Type) OutputOf() Type {
	return t | TypeOutput
}

// ElementType returns the element type of this value.
func (t Type) ElementType() Type {
	return t & elementTypeMask
}

// String returns the string representation of this type.
func (t Type) String() string {
	s := "invalid"
	switch t.ElementType() {
	case TypeBool:
		s = "bool"
	case TypeString:
		s = "string"
	case TypeNumber:
		s = "number"
	case TypeMap:
		s = "map"
	case TypeUnknown:
		s = "unknown"
	default:
		contract.Failf("unknown element type")
	}
	if t.IsList() {
		s = fmt.Sprintf("list<%s>", s)
	}
	if t.IsOutput() {
		s = fmt.Sprintf("output<%s>", s)
	}
	return s

}

// dumper is used to dump bound nodes in a simple S-expression style.
type dumper struct {
	w      io.Writer
	indent string
}

func (d *dumper) indented(f func()) {
	d.indent += "    "
	f()
	d.indent = d.indent[:len(d.indent)-4]
}

func (d *dumper) dump(vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			fmt.Fprint(d.w, v)
		case BoundNode:
			v.dump(d)
		default:
			panic("unexpected type in dump")
		}
	}
}

// Comments represents the set of comments associated with a node.
type Comments struct {
	// Leading is the lines of the comment (sans comment tokens) that precedes a node, if any. Line endings (if any)
	// are present.
	Leading []string
	// Trailing is the lines of the comment (sans comment tokens) that succeeds a node, if any. Line ending (if any)
	// are present.
	Trailing []string
}

// A BoundNode represents a single bound property map, property list, or interpolation expression. Every
// BoundNode has a Type.
type BoundNode interface {
	commentable

	Type() Type
	Comments() *Comments

	dump(d *dumper)
	isNode()
}

// A BoundExpr represents a single node in a bound interpolation expression. This type is used to help ensure that
// bound interpolation expressions only reference nodes that may be present in such expressions.
type BoundExpr interface {
	BoundNode

	isExpr()
}

// BoundArithmetic is the bound form of an HIL arithmetic expression (e.g. `${a + b}`).
type BoundArithmetic struct {
	// Op is the arithmetic operation used by this expression.
	Op ast.ArithmeticOp
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Exprs is the bound list of the arithmetic expression's operands.
	Exprs []BoundExpr
	// ExprType is the type of the arithmetic expression.
	ExprType Type
}

// Type returns the type of the arithmetic expression.
func (n *BoundArithmetic) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundArithmetic) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundArithmetic) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundArithmetic) dump(d *dumper) {
	d.dump("(", fmt.Sprintf("%v %v", n.Type(), n.Op))
	d.indented(func() {
		for _, e := range n.Exprs {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundArithmetic) isNode() {}
func (n *BoundArithmetic) isExpr() {}

// BoundCall is the bound form of an HIL call expression (e.g. `${foo(bar, baz)}`).
type BoundCall struct {
	// Func is the name of the function to call.
	Func string
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// ExprType is the type of the call expression.
	ExprType Type
	// Args is the bound list of the call's arguments.
	Args []BoundExpr
}

// Type returns the type of the call expression.
func (n *BoundCall) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundCall) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundCall) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundCall) dump(d *dumper) {
	d.dump("(call ", fmt.Sprintf("%v %s", n.Type(), n.Func))
	d.indented(func() {
		for _, e := range n.Args {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundCall) isNode() {}
func (n *BoundCall) isExpr() {}

// BoundConditional is the bound form of an HIL conditional expression (e.g. `foo ? bar : baz`).
type BoundConditional struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// ExprType is the type of the conditional expression.
	ExprType Type
	// CondExpr is the bound form of the conditional expression's predicate.
	CondExpr BoundExpr
	// TrueExpr is the bound form of the conditional expression's true branch.
	TrueExpr BoundExpr
	// FalseExpr is the bound from of the condition expression's false branch.
	FalseExpr BoundExpr
}

// Type returns the type of the conditional expression.
func (n *BoundConditional) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundConditional) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundConditional) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundConditional) dump(d *dumper) {
	d.dump("(cond ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		d.dump("\n", d.indent, n.CondExpr)
		d.dump("\n", d.indent, n.TrueExpr)
		d.dump("\n", d.indent, n.FalseExpr)
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundConditional) isNode() {}
func (n *BoundConditional) isExpr() {}

// BoundIndex is the bound form of an HIL index expression (e.g. `${foo[bar]}`).
type BoundIndex struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// ExprType is the type of the index expression.
	ExprType Type
	// TargetExpr is the bound form of the index expression's target (e.g. `foo` in `${foo[bar]}`).
	TargetExpr BoundExpr
	// KeyExpr is the bound form of the index expression's key (e.g. `bar` in `${foo[bar]}`).
	KeyExpr BoundExpr
}

// Type returns the type of the index expression.
func (n *BoundIndex) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundIndex) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundIndex) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundIndex) dump(d *dumper) {
	d.dump("(index ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		d.dump("\n", d.indent, n.TargetExpr)
		d.dump("\n", d.indent, n.KeyExpr)
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundIndex) isNode() {}
func (n *BoundIndex) isExpr() {}

// BoundLiteral is the bound form of a literal value.
type BoundLiteral struct {
	// ExprType is the type of the literal expression.
	ExprType Type
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Value is the value of the literal expression. This may be a bool, string, float64, or in the case of the
	// argument to the __applyArg intrinsic, an int.
	Value interface{}
}

// Type returns the type of the literal expression.
func (n *BoundLiteral) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundLiteral) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundLiteral) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundLiteral) dump(d *dumper) {
	switch n.ExprType {
	case TypeString:
		d.dump(fmt.Sprintf("%q", n.Value))
	default:
		d.dump(fmt.Sprintf("%v", n.Value))
	}
}

func (n *BoundLiteral) isNode() {}
func (n *BoundLiteral) isExpr() {}

// BoundOutput is the bound form of an HIL output expression (e.g. `foo ${bar} baz`).
type BoundOutput struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Exprs is the bound list of the output's operands.
	Exprs []BoundExpr
}

// Type returns the type of the output expression (which is always TypeString).
func (n *BoundOutput) Type() Type {
	return TypeString
}

// Comments returns the comments attached to this node, if any.
func (n *BoundOutput) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundOutput) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundOutput) dump(d *dumper) {
	d.dump("(output ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		for _, e := range n.Exprs {
			d.dump("\n", d.indent, e)
		}
	})
	d.dump("\n", d.indent, ")")
}

func (n *BoundOutput) isNode() {}
func (n *BoundOutput) isExpr() {}

// BoundVariableAccess is the bound form of an HIL variable access expression (e.g. `${foo.bar}`).
type BoundVariableAccess struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Elements are the path elements that comprise the variable access expression.
	Elements []string
	// Schemas are the Terraform and Pulumi schemas associated with the referenced variable.
	Schemas Schemas
	// ExprType is the type of the variable access expression.
	ExprType Type
	// TFVar is the Terraform representation of the variable access expression.
	TFVar config.InterpolatedVariable
	// ILNode is the dependency graph node associated with the accessed variable.
	ILNode Node
}

// Type returns the type of the variable access expression.
func (n *BoundVariableAccess) Type() Type {
	return n.ExprType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundVariableAccess) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundVariableAccess) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundVariableAccess) dump(d *dumper) {
	d.dump(fmt.Sprintf("(%s %s %T)", strings.Join(n.Elements, "."), n.Type(), n.TFVar))
}

func (n *BoundVariableAccess) isNode() {}
func (n *BoundVariableAccess) isExpr() {}

func (n *BoundVariableAccess) IsMissingVariable() bool {
	return n.ILNode == nil
}

// BoundListProperty is the bound form of an HCL list property. (e.g. `[ foo, bar ]`).
type BoundListProperty struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Schemas are the Terraform and Pulumi schemas associated with the list.
	Schemas Schemas
	// Elements is the bound list of the list's elements.
	Elements []BoundNode
}

// Type returns the type of the list property (always a list type).
func (n *BoundListProperty) Type() Type {
	return n.Schemas.ElemSchemas().Type().ListOf()
}

// Comments returns the comments attached to this node, if any.
func (n *BoundListProperty) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundListProperty) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundListProperty) dump(d *dumper) {
	d.dump("(list ", fmt.Sprintf("%v", n.Type()))
	if len(n.Elements) == 0 {
		d.dump(")")
	} else {
		d.indented(func() {
			for _, e := range n.Elements {
				d.dump("\n", d.indent, e)
			}
		})
		d.dump("\n", d.indent, ")")
	}
}

func (n *BoundListProperty) isNode() {}

// BoundMapProperty is the bound form of an HCL map property. (e.g. `{ foo = bar ]`).
type BoundMapProperty struct {
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// Schemas are the Terraform and Pulumi schemas associated with the map.
	Schemas Schemas
	// Elements is a map from name to bound value of the map's elements.
	Elements map[string]BoundNode
}

// Type returns the type of the map property (always TypeMap).
func (n *BoundMapProperty) Type() Type {
	return TypeMap
}

// Comments returns the comments attached to this node, if any.
func (n *BoundMapProperty) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundMapProperty) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundMapProperty) dump(d *dumper) {
	d.dump("(map ", fmt.Sprintf("%v", n.Type()))
	if len(n.Elements) == 0 {
		d.dump(")")
	} else {
		d.indented(func() {
			for k, e := range n.Elements {
				d.dump("\n", d.indent, k, ": ", e)
			}
		})
		d.dump("\n", d.indent, ")")
	}
}

func (n *BoundMapProperty) isNode() {}

// BoundError represents a binding error. This is used to preserve bound values in the case
// of type mismatches and other errors.
type BoundError struct {
	// The type of the node.
	NodeType Type
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// A bound node (if any) associated with this error
	Value BoundNode
	// The binding error
	Error error
}

// Type returns the type of the variable access expression.
func (n *BoundError) Type() Type {
	return n.NodeType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundError) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundError) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundError) dump(d *dumper) {
	d.dump("(error ", fmt.Sprintf("%v", n.Type()))
	if n.Value != nil {
		d.indented(func() {
			d.dump("\n", d.indent, n.Value)
		})
		d.dump("\n", d.indent)
	}
	d.dump(d.indent, fmt.Sprintf("%q)", n.Error.Error()))
}

func (n *BoundError) isNode() {}
func (n *BoundError) isExpr() {}

// BoundPropertyValue wraps a BoundMapProperty or BoundListProperty in a BoundExpr. This is intended primarily for the
// use of transforms that must pass bound properties to intrinsics.
type BoundPropertyValue struct {
	// The type of the node.
	NodeType Type
	// Comments is the set of comments associated with this node, if any.
	NodeComments *Comments
	// The wrapped property.
	Value BoundNode
}

// Type returns the type of the expression.
func (n *BoundPropertyValue) Type() Type {
	return n.NodeType
}

// Comments returns the comments attached to this node, if any.
func (n *BoundPropertyValue) Comments() *Comments {
	return n.NodeComments
}

// setComments attaches the given comments to this node.
func (n *BoundPropertyValue) setComments(c *Comments) {
	n.NodeComments = c
}

func (n *BoundPropertyValue) isNode() {}
func (n *BoundPropertyValue) isExpr() {}

func (n *BoundPropertyValue) dump(d *dumper) {
	d.dump("(propertyExpr ", fmt.Sprintf("%v", n.Type()))
	d.indented(func() {
		d.dump("\n", d.indent, n.Value)
	})
	d.dump("\n", d.indent, ")")
}

// DumpBoundNode dumps the string representation of the given bound node to the given writer.
func DumpBoundNode(w io.Writer, e BoundNode) {
	e.dump(&dumper{w: w})
	fmt.Fprint(w, "\n")
}

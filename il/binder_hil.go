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
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/pkg/errors"

	"github.com/pulumi/tf2pulumi/internal/config"
)

// bindArithmetic binds an HIL arithmetic expression.
func (b *propertyBinder) bindArithmetic(n *ast.Arithmetic) (BoundExpr, error) {
	exprs, err := b.bindExprs(n.Exprs)
	if err != nil {
		return nil, err
	}

	var typ Type
	switch n.Op {
	case ast.ArithmeticOpLogicalAnd, ast.ArithmeticOpLogicalOr,
		ast.ArithmeticOpEqual, ast.ArithmeticOpNotEqual,
		ast.ArithmeticOpLessThan, ast.ArithmeticOpLessThanOrEqual,
		ast.ArithmeticOpGreaterThan, ast.ArithmeticOpGreaterThanOrEqual:
		typ = TypeBool
	default:
		typ = TypeNumber
	}

	return &BoundArithmetic{Op: n.Op, Exprs: exprs, ExprType: typ}, nil
}

// bindCall binds an HIL call expression. This involves binding the call's arguments, then using the name of the called
// function to determine the type of the call expression. The binder curretly only supports a subset of the functions
// supported by terraform.
func (b *propertyBinder) bindCall(n *ast.Call) (BoundExpr, error) {
	args, err := b.bindExprs(n.Args)
	if err != nil {
		return nil, err
	}

	exprType := TypeUnknown
	switch n.Func {
	case "base64decode":
		exprType = TypeString
	case "base64encode":
		exprType = TypeString
	case "chomp":
		exprType = TypeString
	case "cidrhost":
		exprType = TypeString
	case "coalesce":
		exprType = TypeString
	case "coalescelist", "concat":
		if args[0].Type().IsList() {
			exprType = args[0].Type()
		} else {
			exprType = TypeUnknown.ListOf()
		}
	case "compact":
		exprType = TypeString.ListOf()
	case "element":
		if args[0].Type().IsList() {
			exprType = args[0].Type().ElementType()
		}
	case "file":
		exprType = TypeString
	case "format":
		exprType = TypeString
	case "formatlist":
		exprType = TypeString.ListOf()
	case "indent":
		exprType = TypeString
	case "join":
		exprType = TypeString
	case "length":
		exprType = TypeNumber
	case "list":
		exprType = TypeUnknown.ListOf()
	case "lookup":
		// nothing to do
	case "lower":
		exprType = TypeString
	case "map":
		if len(args)%2 != 0 {
			err = errors.Errorf("the number of arguments to \"map\" must be even")
		}
		exprType = TypeMap
	case "merge":
		exprType = TypeMap
	case "min":
		exprType = TypeNumber
	case "replace":
		exprType = TypeString
	case "signum":
		exprType = TypeNumber
	case "split":
		exprType = TypeString.ListOf()
	case "substr":
		exprType = TypeString
	case "zipmap":
		exprType = TypeMap
	default:
		err = errors.Errorf("NYI: call to %s", n.Func)
	}

	boundCall := &BoundCall{Func: n.Func, ExprType: exprType, Args: args}
	if err != nil {
		return &BoundError{Value: boundCall, NodeType: exprType, Error: err}, nil
	}
	return boundCall, nil
}

// bindConditional binds an HIL conditional expression.
func (b *propertyBinder) bindConditional(n *ast.Conditional) (BoundExpr, error) {
	condExpr, err := b.bindExpr(n.CondExpr)
	if err != nil {
		return nil, err
	}
	trueExpr, err := b.bindExpr(n.TrueExpr)
	if err != nil {
		return nil, err
	}
	falseExpr, err := b.bindExpr(n.FalseExpr)
	if err != nil {
		return nil, err
	}

	// If the types of both branches match, then the type of the expression is that of the branches. If the types of
	// both branches differ, then mark the type as unknown.
	exprType := trueExpr.Type()
	if exprType != falseExpr.Type() {
		exprType = TypeUnknown
	}

	return &BoundConditional{
		ExprType:  exprType,
		CondExpr:  condExpr,
		TrueExpr:  trueExpr,
		FalseExpr: falseExpr,
	}, nil
}

// bindIndex binds an HIL index expression.
func (b *propertyBinder) bindIndex(n *ast.Index) (BoundExpr, error) {
	boundTarget, err := b.bindExpr(n.Target)
	if err != nil {
		return nil, err
	}
	boundKey, err := b.bindExpr(n.Key)
	if err != nil {
		return nil, err
	}

	// If the target type is not a list, then the type of the expression is unknown. Otherwise it is the element type
	// of the list.
	exprType := TypeUnknown
	targetType := boundTarget.Type()
	if targetType.IsList() {
		exprType = targetType.ElementType()
	}

	return &BoundIndex{
		ExprType:   exprType,
		TargetExpr: boundTarget,
		KeyExpr:    boundKey,
	}, nil
}

// bindLiteral binds an HIL literal expression. The literal must be of type bool, int, float, or string.
func (b *propertyBinder) bindLiteral(n *ast.LiteralNode) (BoundExpr, error) {
	var exprType Type
	value := n.Value
	switch n.Typex {
	case ast.TypeBool:
		exprType = TypeBool
	case ast.TypeInt:
		exprType, value = TypeNumber, float64(value.(int))
	case ast.TypeFloat:
		exprType = TypeNumber
	case ast.TypeString:
		exprType = TypeString
	default:
		return nil, errors.Errorf("Unexpected literal type %v", n.Typex)
	}

	return &BoundLiteral{ExprType: exprType, Value: value}, nil
}

// bindOutput binds an HIL output expression.
func (b *propertyBinder) bindOutput(n *ast.Output) (BoundExpr, error) {
	exprs, err := b.bindExprs(n.Exprs)
	if err != nil {
		return nil, err
	}

	// Project a single-element output to the element itself.
	if len(exprs) == 1 {
		return exprs[0], nil
	}

	return &BoundOutput{Exprs: exprs}, nil
}

// bindVariableAccess binds an HIL variable access expression. This involves first interpreting the variable name as a
// Terraform interpolated variable, then using the result of that interpretation to decide which graph node the
// variable access refers to, if any: count, path, and Terraformn variables may not refer to graph nodes. It is an
// error for a variable access to refer to a non-existent node.
func (b *propertyBinder) bindVariableAccess(n *ast.VariableAccess) (BoundExpr, error) {
	tfVar, err := config.NewInterpolatedVariable(n.Name)
	if err != nil {
		return nil, err
	}

	elements, sch, exprType, ilNode := []string(nil), Schemas{}, TypeUnknown, Node(nil)
	switch v := tfVar.(type) {
	case *config.CountVariable:
		// "count."
		if v.Type != config.CountValueIndex {
			return nil, errors.Errorf("unsupported count variable %s", v.FullKey())
		}

		if !b.hasCountIndex {
			return &BoundLiteral{ExprType: TypeNumber, Value: 1.0}, nil
		}

		exprType = TypeNumber
	case *config.LocalVariable:
		// "local."
		l, ok := b.builder.locals[v.Name]
		if !ok {
			if b.builder.allowMissingVariables {
				return &BoundVariableAccess{
					ExprType: TypeUnknown,
					TFVar:    v,
				}, nil
			}
			return nil, errors.Errorf("unknown local %v", v.Name)
		}
		ilNode = l

		// Ensure that the referenced local has been bound before attempting to inspect its value.
		if err := b.builder.ensureBound(l); err != nil {
			return nil, err
		}

		exprType = l.Value.Type()
	case *config.ModuleVariable:
		// "module."
		m, ok := b.builder.modules[v.Name]
		if !ok {
			if b.builder.allowMissingVariables {
				return &BoundVariableAccess{
					ExprType: TypeUnknown.OutputOf(),
					TFVar:    v,
				}, nil
			}
			return nil, errors.Errorf("unknown module %v", v.Name)
		}
		ilNode = m

		exprType = TypeUnknown.OutputOf()
	case *config.PathVariable:
		// "path."
		exprType = TypeString
	case *config.ResourceVariable:
		// default

		// Split the path elements.
		elements = strings.Split(v.Field, ".")

		// Look up the resource.
		r, ok := b.builder.resources[v.ResourceId()]
		if !ok {
			if b.builder.allowMissingVariables {
				return &BoundVariableAccess{
					Elements: elements,
					ExprType: TypeUnknown.OutputOf(),
					TFVar:    v,
				}, nil
			}
			return nil, errors.Errorf("unknown resource %v", v.ResourceId())
		}
		ilNode = r

		// Ensure that the resource has a provider.
		if err := b.builder.ensureBound(r); err != nil {
			return nil, err
		}

		// Fetch the resource's schema info.
		sch = r.Schemas()

		// Parse the path of the accessed field (name{.property}+).
		elemSch := sch
		for _, e := range elements {
			elemSch = elemSch.PropertySchemas(e)
		}

		// If this access refers to a counted resource but is not itself a splat or an index, treat it as if it is
		// accessing the first resource. This is roughly consistent with TF, which allows the following:
		//
		//     resource "foo_resource" "foo" {
		//         count = "${cond} ? 0 : 1"
		//     }
		//
		//     resource "bar_resource" "bar" {
		//         foo_name = "${foo_resource.foo.name}"
		//     }
		//
		// Note that this only works in TF when the target's count is exactly 1.
		if r.Count != nil && !v.Multi {
			v.Multi, v.Index = true, 0
		}

		// If this access refers to a non-counted resource but is a multi-access or an index, treat it as if it is
		// a normal access.
		if r.Count == nil && v.Multi {
			v.Multi = false
		}

		// Handle multi-references (splats and indexes).
		exprType = elemSch.Type().OutputOf()
		if v.Multi && v.Index == -1 {
			exprType = exprType.ListOf()
		}
	case *config.SelfVariable:
		// "self."
		return nil, errors.New("NYI: self variables")
	case *config.SimpleVariable:
		// "[^.]\+"
		return nil, errors.New("NYI: simple variables")
	case *config.TerraformVariable:
		if v.Field != "workspace" {
			return nil, errors.Errorf("unsupported key 'terraform.%s'", v.Field)
		}
		return NewGetStackCall(), nil
	case *config.UserVariable:
		// "var."
		if v.Elem != "" {
			return nil, errors.New("NYI: user variable elements")
		}

		// Look up the variable.
		vn, ok := b.builder.variables[v.Name]
		if !ok {
			if b.builder.allowMissingVariables {
				return &BoundVariableAccess{
					ExprType: TypeString,
					TFVar:    v,
				}, nil
			}
			return nil, errors.Errorf("unknown variable %s", v.Name)
		}
		ilNode = vn

		// If the variable does not have a default, its type is string. If it does have a default, its type is the type
		// of the default.
		exprType = TypeString
		if vn.DefaultValue != nil {
			exprType = vn.DefaultValue.Type()
		}
	default:
		return nil, errors.Errorf("unexpected variable type %T", v)
	}

	return &BoundVariableAccess{
		Elements: elements,
		Schemas:  sch,
		ExprType: exprType,
		TFVar:    tfVar,
		ILNode:   ilNode,
	}, nil
}

// bindExprs binds the list of HIL expressions and returns the resulting list.
func (b *propertyBinder) bindExprs(ns []ast.Node) ([]BoundExpr, error) {
	boundExprs := make([]BoundExpr, len(ns))
	for i, n := range ns {
		bn, err := b.bindExpr(n)
		if err != nil {
			return nil, err
		}
		boundExprs[i] = bn
	}
	return boundExprs, nil
}

// bindExpr binds a single HIL expression.
func (b *propertyBinder) bindExpr(n ast.Node) (BoundExpr, error) {
	switch n := n.(type) {
	case *ast.Arithmetic:
		return b.bindArithmetic(n)
	case *ast.Call:
		return b.bindCall(n)
	case *ast.Conditional:
		return b.bindConditional(n)
	case *ast.Index:
		return b.bindIndex(n)
	case *ast.LiteralNode:
		return b.bindLiteral(n)
	case *ast.Output:
		return b.bindOutput(n)
	case *ast.VariableAccess:
		return b.bindVariableAccess(n)
	default:
		return nil, errors.Errorf("unexpected HIL node type %T", n)
	}
}

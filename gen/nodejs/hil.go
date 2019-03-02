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

package nodejs

import (
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/hil/ast"
	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

// This file contains the code necessary to generate code for bound expression trees. It is the responsibility of each
// node-specific generation function to ensure that the generated code is appropriately parenthesized where necessary
// in order to avoid unexpected issues with operator precedence.

// GenArithmetic generates code for the given arithmetic expression.
func (g *generator) GenArithmetic(w io.Writer, n *il.BoundArithmetic) {
	op := ""
	switch n.HILNode.Op {
	case ast.ArithmeticOpAdd:
		op = "+"
	case ast.ArithmeticOpSub:
		op = "-"
	case ast.ArithmeticOpMul:
		op = "*"
	case ast.ArithmeticOpDiv:
		op = "/"
	case ast.ArithmeticOpMod:
		op = "%"
	case ast.ArithmeticOpLogicalAnd:
		op = "&&"
	case ast.ArithmeticOpLogicalOr:
		op = "||"
	case ast.ArithmeticOpEqual:
		op = "==="
	case ast.ArithmeticOpNotEqual:
		op = "!=="
	case ast.ArithmeticOpLessThan:
		op = "<"
	case ast.ArithmeticOpLessThanOrEqual:
		op = "<="
	case ast.ArithmeticOpGreaterThan:
		op = ">"
	case ast.ArithmeticOpGreaterThanOrEqual:
		op = ">="
	}
	op = fmt.Sprintf(" %s ", op)

	g.Fgen(w, "(")
	for i, n := range n.Exprs {
		if i != 0 {
			g.Fgen(w, op)
		}
		g.Fgen(w, n)
	}
	g.Fgen(w, ")")
}

// genApplyOutput generates code for a single argument to a `.apply` invocation.
func (g *generator) genApplyOutput(w io.Writer, n *il.BoundVariableAccess) {
	if rv, ok := n.TFVar.(*config.ResourceVariable); ok && rv.Multi && rv.Index == -1 {
		g.Fgenf(w, "pulumi.all(%v)", n)
	} else {
		g.Fgen(w, n)
	}
}

// genApply generates code for a single `.apply` invocation as represented by a call to the `__apply` intrinsic.
func (g *generator) genApply(w io.Writer, n *il.BoundCall) {
	// Extract the list of outputs and the continuation expression from the `__apply` arguments.
	applyArgs, then := il.ParseApplyCall(n)
	g.applyArgs, g.applyArgNames = applyArgs, g.assignApplyArgNames(applyArgs, then)

	if len(g.applyArgs) == 1 {
		// If we only have a single output, just generate a normal `.apply`.
		g.genApplyOutput(w, g.applyArgs[0])
		g.Fgenf(w, ".apply(%s => %v)", g.applyArgNames[0], then)
	} else {
		// Otherwise, generate a call to `pulumi.all([]).apply()`.
		g.Fgen(w, "pulumi.all([")
		for i, o := range g.applyArgs {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.genApplyOutput(w, o)
		}
		g.Fgen(w, "]).apply(([")
		for i := range g.applyArgs {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgenf(w, "%s", g.applyArgNames[i])
		}
		g.Fgen(w, "]) => ", then, ")")
	}

	// Finally, clear the current set of apply arguments.
	g.applyArgs = nil
}

// genApplyArg generates a single reference to a resolved output value inside the context of a call top `.apply`.
func (g *generator) genApplyArg(w io.Writer, index int) {
	contract.Assert(g.applyArgs != nil)

	// Extract the variable reference.
	v := g.applyArgs[index]

	// Generate a reference to the parameter.
	g.Fgen(w, g.applyArgNames[index])

	// Generate any nested path.
	if rv, ok := v.TFVar.(*config.ResourceVariable); ok {
		// Handle splats
		isSplat := rv.Multi && rv.Index == -1
		if isSplat {
			g.Fgen(w, ".map(v => v")
		}

		sch, elements := v.Schemas, v.Elements
		if g.resourceMode(v) == config.ManagedResourceMode {
			sch, elements = sch.PropertySchemas(elements[0]), elements[1:]
		} else if r, ok := v.ILNode.(*il.ResourceNode); ok && r.Provider.Config.Name == "http" {
			elements = nil
		}
		for _, e := range elements {
			isListElement := sch.Type().IsList()
			projectListElement := isListElement && tfbridge.IsMaxItemsOne(sch.TF, sch.Pulumi)

			sch = sch.PropertySchemas(e)
			if isListElement {
				// If we're projecting the list element, just skip this path element entirely.
				if !projectListElement {
					g.Fgenf(w, "[%s]", e)
				}
			} else {
				g.Fgenf(w, ".%s", tfbridge.TerraformToPulumiName(e, sch.TF, false))
			}
		}

		if isSplat {
			g.Fgen(w, ")")
		}
	}
}

// genCoercion generates code for a single call to the __coerce intrinsic that converts an expression between types.
func (g *generator) genCoercion(w io.Writer, n il.BoundExpr, toType il.Type) {
	switch n.Type() {
	case il.TypeBool:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.Fgenf(w, "\"%v\"", lit)
			} else {
				g.Fgenf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeNumber:
		if toType == il.TypeString {
			if lit, ok := n.(*il.BoundLiteral); ok {
				g.Fgenf(w, "\"%v\"", lit)
			} else {
				g.Fgenf(w, "`${%v}`", n)
			}
			return
		}
	case il.TypeString:
		switch toType {
		case il.TypeBool:
			g.Fgenf(w, "(%v === \"true\")", n)
			return
		case il.TypeNumber:
			g.Fgenf(w, "Number.parseFloat(%v)", n)
			return
		}
	}

	// If we get here, we weren't able to genereate a coercion. Just generate the node. This is questionable behavior
	// at best.
	g.Fgen(w, n)
}

// GenCall generates code for a call expression.
func (g *generator) GenCall(w io.Writer, n *il.BoundCall) {
	switch n.HILNode.Func {
	case il.IntrinsicApply:
		g.genApply(w, n)
	case il.IntrinsicApplyArg:
		g.genApplyArg(w, il.ParseApplyArgCall(n))
	case il.IntrinsicArchive:
		g.Fgenf(w, "new pulumi.asset.FileArchive(%v)", il.ParseArchiveCall(n))
	case il.IntrinsicAsset:
		g.Fgenf(w, "new pulumi.asset.FileAsset(%v)", il.ParseAssetCall(n))
	case il.IntrinsicCoerce:
		value, toType := il.ParseCoerceCall(n)
		g.genCoercion(w, value, toType)
	case il.IntrinsicGetStack:
		g.Fgenf(w, "pulumi.getStack()")
	case intrinsicDataSource:
		function, inputs := parseDataSourceCall(n)
		g.Fgenf(w, "%s(%s)", function, inputs)
	case "base64decode":
		g.Fgenf(w, "Buffer.from(%v, \"base64\").toString()", n.Args[0])
	case "base64encode":
		g.Fgenf(w, "Buffer.from(%v).toString(\"base64\")", n.Args[0])
	case "chomp":
		g.Fgenf(w, "%v.replace(/(\\n|\\r\\n)*$/, \"\")", n.Args[0])
	case "coalesce":
		g.Fgen(w, "[")
		for i, v := range n.Args {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgen(w, v)
		}
		g.Fgen(w, "].find((v: any) => v !== undefined && v !== \"\")")
	case "coalescelist":
		g.Fgen(w, "[")
		for i, v := range n.Args {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgen(w, v)
		}
		g.Fgen(w, "].find((v: any) => v !== undefined && (<any[]>v).length > 0)")
	case "compact":
		g.Fgenf(w, "%v.filter((v: any) => <string>v !== \"\")", n.Args[0])
	case "concat":
		g.Fgenf(w, "%v.concat(", n.Args[0])
		for i, arg := range n.Args[1:] {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgenf(w, "%v", arg)
		}
		g.Fgen(w, ")")
	case "element":
		g.Fgenf(w, "%v[%v]", n.Args[0], n.Args[1])
	case "file":
		g.Fgenf(w, "fs.readFileSync(%v, \"utf-8\")", n.Args[0])
	case "format":
		g.Fgen(w, "sprintf.sprintf(")
		for i, a := range n.Args {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgen(w, a)
		}
		g.Fgen(w, ")")
	case "indent":
		g.Fgenf(w,
			"((str, indent) => str.split(\"\\n\").map((l, i) => i == 0 ? l : indent + l).join(\"\"))(%v, \" \".repeat(%v))",
			n.Args[1], n.Args[0])
	case "join":
		g.Fgenf(w, "%v.join(%v)", n.Args[1], n.Args[0])
	case "length":
		g.Fgenf(w, "%v.length", n.Args[0])
	case "list":
		g.Fgen(w, "[")
		for i, e := range n.Args {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgen(w, e)
		}
		g.Fgen(w, "]")
	case "lookup":
		hasDefault := len(n.Args) == 3
		if hasDefault {
			g.Fgen(w, "(")
		}
		g.Fgenf(w, "(<any>%v)[%v]", n.Args[0], n.Args[1])
		if hasDefault {
			g.Fgenf(w, " || %v)", n.Args[2])
		}
	case "lower":
		g.Fgenf(w, "%v.toLowerCase()", n.Args[0])
	case "map":
		contract.Assert(len(n.Args)%2 == 0)
		g.Fgen(w, "{")
		for i := 0; i < len(n.Args); i += 2 {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			if lit, ok := n.Args[i].(*il.BoundLiteral); ok && lit.Type() == il.TypeString {
				g.Fgen(w, lit)
			} else {
				g.Fgenf(w, "[%v]", n.Args[i])
			}
			g.Fgenf(w, ": %v", n.Args[i+1])
		}
		g.Fgen(w, "}")
	case "merge":
		g.Fgenf(w, "Object.assign(%v", n.Args[0])
		for i, arg := range n.Args[1:] {
			if i > 0 {
				g.Fgen(w, ", ")
			}
			g.Fgenf(w, "%v", arg)
		}
		g.Fgen(w, ")")
	case "min":
		g.Fgenf(w, "%v.reduce((min, v) => !min ? v : Math.min(min, v))", n.Args[0])
	case "replace":
		pat := (interface{})(n.Args[1])
		if lit, ok := pat.(*il.BoundLiteral); ok && lit.Type() == il.TypeString {
			patStr := lit.Value.(string)
			if len(patStr) > 1 && patStr[0] == '/' && patStr[1] == '/' {
				pat = patStr
			}
		}
		g.Fgenf(w, "%v.replace(%v, %v)", n.Args[0], pat, n.Args[2])
	case "signum":
		g.Fgenf(w, "Math.sign(%v)", n.Args[0])
	case "split":
		g.Fgenf(w, "%v.split(%v)", n.Args[1], n.Args[0])
	case "substr":
		g.Fgenf(w, "((str, s, l) => str.slice(s, l === -1 ? s.length : s + l))(%v, %v, %v)", n.Args[0], n.Args[1], n.Args[2])
	case "zipmap":
		g.Fgenf(w, "((keys, values) => Object.assign.apply({}, keys.map((k: any, i: number) => ({[k]: values[i]}))))(%v, %v)",
			n.Args[0], n.Args[1])
	default:
		g.Fgenf(w, "(() => { throw \"NYI: call to %v\"; })()", n.HILNode.Func)
	}
}

// GenConditional generates code for a single conditional expression.
func (g *generator) GenConditional(w io.Writer, n *il.BoundConditional) {
	g.Fgenf(w, "(%v ? %v : %v)", n.CondExpr, n.TrueExpr, n.FalseExpr)
}

// GenIndex generates code for a single index expression.
func (g *generator) GenIndex(w io.Writer, n *il.BoundIndex) {
	g.Fgenf(w, "%v[%v]", n.TargetExpr, n.KeyExpr)
}

func (g *generator) genStringLiteral(w io.Writer, v string) {
	builder := strings.Builder{}
	newlines := strings.Count(v, "\n")
	if newlines == 0 || newlines == 1 && (v[0] == '\n' || v[len(v)-1] == '\n') {
		// This string either does not contain newlines or contains a single leading or trailing newline, so we'll
		// generate a normal string literal. Quotes, backslashes, and newlines will be escaped in conformance with
		// ECMA-262 11.8.4 ("String Literals").
		builder.WriteRune('"')
		for _, c := range v {
			if c == '\n' {
				builder.WriteString(`\n`)
			} else {
				if c == '"' || c == '\\' {
					builder.WriteRune('\\')
				}
				builder.WriteRune(c)
			}
		}
		builder.WriteRune('"')
	} else {
		// This string does contain newlines, so we'll generate a template string literal. "${", backquotes, and
		// backslashes will be escaped in conformance with ECMA-262 11.8.6 ("Template Literal Lexical Components").
		runes := []rune(v)
		builder.WriteRune('`')
		for i, c := range runes {
			switch c {
			case '$':
				if i < len(runes)-1 && runes[i+1] == '{' {
					builder.WriteRune('\\')
				}
			case '`', '\\':
				builder.WriteRune('\\')
			}
			builder.WriteRune(c)
		}
		builder.WriteRune('`')
	}

	g.Fgenf(w, "%s", builder.String())
}

// GenLiteral generates code for a single literal expression
func (g *generator) GenLiteral(w io.Writer, n *il.BoundLiteral) {
	switch n.ExprType {
	case il.TypeBool:
		g.Fgenf(w, "%v", n.Value)
	case il.TypeNumber:
		f := n.Value.(float64)
		if float64(int64(f)) == f {
			g.Fgenf(w, "%d", int64(f))
		} else {
			g.Fgenf(w, "%g", n.Value)
		}
	case il.TypeString:
		g.genStringLiteral(w, n.Value.(string))
	default:
		contract.Failf("unexpected literal type in genLiteral: %v", n.ExprType)
	}
}

// GenOutput generates code for a single output expression.
func (g *generator) GenOutput(w io.Writer, n *il.BoundOutput) {
	g.Fgen(w, "`")
	for _, s := range n.Exprs {
		if lit, ok := s.(*il.BoundLiteral); ok && lit.ExprType == il.TypeString {
			g.Fgen(w, lit.Value.(string))
		} else {
			g.Fgenf(w, "${%v}", s)
		}
	}
	g.Fgen(w, "`")
}

// GenPropertyValue generates code for a single property value expression.
func (g *generator) GenPropertyValue(w io.Writer, n *il.BoundPropertyValue) {
	g.Gen(w, n.Value)
}

// GenVariableAccess generates code for a single variable access expression.
func (g *generator) GenVariableAccess(w io.Writer, n *il.BoundVariableAccess) {
	switch v := n.TFVar.(type) {
	case *config.CountVariable, *config.LocalVariable, *config.UserVariable:
		g.Fgen(w, g.variableName(n))

	case *config.ModuleVariable:
		g.Fgen(w, g.variableName(n))
		for _, e := range strings.Split(v.Field, ".") {
			g.Fgenf(w, ".%s", tfbridge.TerraformToPulumiName(e, nil, false))
		}
	case *config.PathVariable:
		switch v.Type {
		case config.PathValueCwd:
			g.Fgen(w, "process.cwd()")
		case config.PathValueModule:
			contract.Failf("modules path references should have been lowered to literals")
		case config.PathValueRoot:
			contract.Failf("root path references should have been lowered to literals")
		}
	case *config.ResourceVariable:
		// We only generate up to the "output" part of the path here: the apply transform will take care of the rest.
		g.Fgen(w, g.variableName(n))
		if v.Multi && v.Index != -1 {
			g.Fgenf(w, "[%d]", v.Index)
		}

		// A managed resource is not itself an output: instead, it is a bag of outputs. As such, we must generate the
		// first portion of this access.
		if g.resourceMode(n) == config.ManagedResourceMode && len(n.Elements) > 0 {
			element := n.Elements[0]
			elementSch := n.Schemas.PropertySchemas(element)

			// Handle splats
			isSplat := v.Multi && v.Index == -1
			if isSplat {
				g.Fgen(w, ".map(v => v")
			}
			g.Fgenf(w, ".%s", tfbridge.TerraformToPulumiName(element, elementSch.TF, false))
			if isSplat {
				g.Fgen(w, ")")
			}
		}
	default:
		contract.Failf("unexpected TF var type in genVariableAccess: %T", n.TFVar)
	}
}

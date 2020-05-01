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
	"strings"

	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
)

type applyNameTable struct {
	g          *generator
	assigned   map[string]bool
	nameCounts map[string]int
}

// tsName computes the TypeScript form of the given name.
func (nt *applyNameTable) tsName(name string) string {
	return camel(tsName(name, nil, nil, false))
}

// disambiguate ensures that the given name is unambiguous by appending an integer starting with 1 if necessary.
func (nt *applyNameTable) disambiguate(name string) string {
	if name == "" {
		name = "arg"
	} else if isReservedWord(name) {
		name = "_" + name
	}

	if !nt.assigned[name] {
		return name
	}

	root := name
	for i := 1; nt.nameCounts[name] != 0; i++ {
		name = fmt.Sprintf("%s%d", root, i)
	}
	return name
}

// bestArgName computes the "best" name for a given apply argument. If this name is unambiguous after all best names
// have been calculated, it will be assigned to the argument. Otherwise, it will go through the disambiguation process
// in disambiguateArgName.
func (nt *applyNameTable) bestArgName(n *il.BoundVariableAccess) string {
	switch v := n.TFVar.(type) {
	case *config.LocalVariable, *config.UserVariable:
		// Use the variable's name.
		return nt.g.variableName(n)
	case *config.ModuleVariable:
		// Use the name of the path's first field, which is the name of the output-typed field argument.
		fields := strings.Split(v.Field, ".")
		return nt.tsName(fields[0])
	case *config.ResourceVariable:
		// If dealing with a data source or a broken access, use the resource's variable name.
		if nt.g.isDataSourceAccess(n) || len(n.Elements) == 0 {
			return nt.g.variableName(n)
		}

		// Otherwise, use the name of the path's first field, which is the name of the output-typed field argument.
		element := n.Elements[0]
		elementSch := n.Schemas.PropertySchemas(element)
		return tfbridge.TerraformToPulumiName(element, elementSch.TF, nil, false)
	default:
		// Path and Count variables should never be Output-typed.
		contract.Failf("unexpected TF var type in assignApplyArgName: %T", v)
		return ""
	}
}

// disambiguateArgName applies type-specific disambiguation to an argument name.
func (nt *applyNameTable) disambiguateArgName(n *il.BoundVariableAccess, bestName string) string {
	switch n.TFVar.(type) {
	case *config.ModuleVariable:
		// Attempt to disambiguate by prepending the module's variable name.
		return nt.disambiguate(nt.g.variableName(n) + title(bestName))
	case *config.ResourceVariable:
		// If dealing with a data source, hand off to the generic disambiguator. Otherwise, attempt to disambiguate
		// by prepending the resource's variable name.
		if nt.g.isDataSourceAccess(n) || len(n.Elements) == 0 {
			return nt.disambiguate(bestName)
		}
		return nt.disambiguate(nt.g.variableName(n) + title(bestName))
	default:
		// Hand off to the generic disambiguator.
		return nt.disambiguate(bestName)
	}
}

func (g *generator) assignApplyArgNames(applyArgs []*il.BoundVariableAccess, then il.BoundExpr) []string {
	nt := &applyNameTable{
		g:          g,
		assigned:   make(map[string]bool),
		nameCounts: make(map[string]int),
	}

	// We do this in two passes:
	// - In the first pass, we find all ambiguous names
	// - In the second pass, we disambiguate names as necessary
	_, err := il.VisitBoundExpr(then, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if v, ok := n.(*il.BoundVariableAccess); ok {
			// The apply rewriter should have ensured that no variable accesses remain that are output-typed.
			contract.Assert(!v.Type().IsOutput())

			// If this is a reference to a named variable, put the name in scope.
			if name := g.variableName(v); name != "" {
				nt.assigned[name], nt.nameCounts[name] = true, 1
			}
		}
		return n, nil
	})
	contract.AssertNoError(err)

	argNames := make([]string, len(applyArgs))
	for i, arg := range applyArgs {
		bestName := nt.bestArgName(arg)
		argNames[i], nt.nameCounts[bestName] = bestName, nt.nameCounts[bestName]+1
	}

	for i, argName := range argNames {
		if nt.nameCounts[argName] > 1 {
			argName = nt.disambiguateArgName(applyArgs[i], argName)
			if nt.nameCounts[argName] == 0 {
				nt.nameCounts[argName] = 1
			}
			argNames[i], nt.assigned[argName] = argName, true
		}
	}
	return argNames
}

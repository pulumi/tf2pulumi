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
	"unicode"
	"unicode/utf8"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

type nameTable struct {
	names        map[il.Node]string
	assigned     map[string]bool
	isRootModule bool
}

// isReservedWord returns true if s is a reserved word as per ECMA-262.
func isReservedWord(s string) bool {
	switch s {
	case "break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete",
		"do", "else", "export", "extends", "finally", "for", "function", "if", "import",
		"in", "instanceof", "new", "return", "super", "switch", "this", "throw", "try",
		"typeof", "var", "void", "while", "with", "yield":
		// Keywords
		return true

	case "enum", "await", "implements", "interface", "package", "private", "protected", "public":
		// Future reserved words
		return true

	case "null", "true", "false":
		// Null and boolean literals
		return true

	default:
		return false
	}
}

// title replaces the first character in the given string with its upper-case equivalent.
func title(s string) string {
	c, sz := utf8.DecodeRuneInString(s)
	if sz == 0 || unicode.IsUpper(c) {
		return s
	}
	return string([]rune{unicode.ToUpper(c)}) + s[sz:]
}

// camel replaces the first character in the given string with its lower-case equivalent.
func camel(s string) string {
	c, sz := utf8.DecodeRuneInString(s)
	if sz == 0 || unicode.IsLower(c) {
		return s
	}
	return string([]rune{unicode.ToLower(c)}) + s[sz:]
}

// tsName computes the TypeScript form of the given name.
func (nt *nameTable) tsName(name string) (string, bool) {
	n := camel(tsName(name, nil, nil, false))
	return n, isReservedWord(n)
}

// disambiguate ensures that the given name is unambiguous by appending an integer starting with 1 if necessary.
func (nt *nameTable) disambiguate(name string) string {
	root := name
	for i := 1; nt.assigned[name]; i++ {
		name = fmt.Sprintf("%s%d", root, i)
	}
	return name
}

// assignOutput assigns an unambiguous name to an output node.
func (nt *nameTable) assignOutput(n *il.OutputNode) {
	// We use the global tsName function here so that we can pass an argument for isObjectKey.
	name := tsName(n.Name, nil, nil, !nt.isRootModule)
	contract.Assert(!nt.assigned[name])

	nt.names[n] = name

	// Outputs do not share the same namespace as other nodes if we are generating a child module.
	if nt.isRootModule {
		nt.assigned[name] = true
	}
}

// assignLocal assigns an unambiguous name to a local node.
func (nt *nameTable) assignLocal(n *il.LocalNode) {
	name, isReserved := nt.tsName(n.Name)

	// If the raw name is reserved or ambiguous, first attempt to disambiguate by prepending "my".
	if isReserved || nt.assigned[name] {
		name = nt.disambiguate("my" + strings.Title(name))
	}

	nt.names[n], nt.assigned[name] = name, true
}

// assignVariable assigns an unambiguous name to a variable node.
func (nt *nameTable) assignVariable(n *il.VariableNode) {
	name, isReserved := nt.tsName(n.Name)

	// If the raw name is reserved or ambiguous, first attempt to disambiguate by appending "Input".
	if isReserved || nt.assigned[name] {
		name = nt.disambiguate(name + "Input")
	}

	nt.names[n], nt.assigned[name] = name, true
}

// assignModule assigns an unambiguous name to a module node.
func (nt *nameTable) assignModule(n *il.ModuleNode) {
	name, isReserved := nt.tsName(n.Name)

	// If the raw name is ambiguous, first attempt to disambiguate by appending "Instance"
	if isReserved || nt.assigned[name] {
		name = nt.disambiguate(name + "Instance")
	}

	nt.names[n], nt.assigned[name] = name, true
}

// assignProvider assigns an unambiguous name to a provider node.
func (nt *nameTable) assignProvider(n *il.ProviderNode) {
	name, isReserved := nt.tsName(n.Alias)

	// If the raw name is ambiguous, first attempt to disambiguate by prepending the package name.
	if isReserved || nt.assigned[name] {
		name = nt.disambiguate(n.PluginName + title(name))
	}

	nt.names[n], nt.assigned[name] = name, true
}

// disambiguateResourceName computes an unambiguous name for the given resource node.
func (nt *nameTable) disambiguateResourceName(n *il.ResourceNode) string {
	name, isReserved := nt.tsName(n.Name)

	if len(name) == 1 {
		// If the name is a single character, ignore it and fall through to the disambiguator.
		name = ""
	} else if name != "" && !isReserved && !nt.assigned[name] {
		// If the name is not reserved and is unambiguous, use it.
		return name
	}

	// Determine the resource's NodeJS package, module, and type name. These will be used during the disambiguation
	// process. If these names cannot be determined, return an ugly name comprised of the TF type and name.
	packageName, moduleName, typeName, err := resourceTypeName(n)
	if err != nil {
		return cleanName(n.Type + "_" + n.Name)
	}
	packageName, moduleName = title(packageName), title(moduleName)

	// If we're dealing with a data source, strip any leading "get" from the typeName.
	if n.IsDataSource && strings.HasPrefix(typeName, "get") {
		typeName = typeName[len("get"):]
	}

	// First attempt to disambiguate by appending the NodeJS resource type.
	root := name
	name = camel(root + typeName)
	if !nt.assigned[name] {
		return name
	}

	// Next, attempt to disambiguate by appending the NodeJS module and type.
	name = camel(root + moduleName + typeName)
	if !nt.assigned[name] {
		return name
	}

	// Finally, attempt to disambiguate by appending the NodeJS package, module, and type.
	return nt.disambiguate(camel(root + packageName + moduleName + typeName))
}

// assignResource assigns an unambiguous name to a resource node.
func (nt *nameTable) assignResource(n *il.ResourceNode) {
	name := nt.disambiguateResourceName(n)
	nt.names[n], nt.assigned[name] = name, true
}

func assignNames(g *il.Graph, importNames map[string]bool, isRootModule bool) map[il.Node]string {
	nt := &nameTable{
		names:        make(map[il.Node]string),
		assigned:     make(map[string]bool),
		isRootModule: isRootModule,
	}

	// Seed the set of assigned names with the names of imported modules.
	for k := range importNames {
		nt.assigned[k] = true
	}

	// Assign output names first: given a conflict between nodes, we always want the output node (if any) to win so
	// that output names are predictable and as consistent with their TF names as is possible.
	for _, k := range gen.SortedKeys(g.Outputs) {
		nt.assignOutput(g.Outputs[k])
	}

	// Next, record all other nodes in the following order:
	// 1. Locals
	// 2. Variables
	// 3. Modules
	// 4. Providers
	// 5. Resources
	for _, k := range gen.SortedKeys(g.Locals) {
		nt.assignLocal(g.Locals[k])
	}
	for _, k := range gen.SortedKeys(g.Variables) {
		nt.assignVariable(g.Variables[k])
	}
	for _, k := range gen.SortedKeys(g.Modules) {
		nt.assignModule(g.Modules[k])
	}
	for _, k := range gen.SortedKeys(g.Providers) {
		nt.assignProvider(g.Providers[k])
	}

	// We handle resources in two passes: in the first pass, we decide which names are ambiguous, and in the second pass
	// we assign names. We do this so that we can apply disambiguation more uniformly across resource names.
	resourceGroups := make(map[string][]*il.ResourceNode)
	for _, k := range gen.SortedKeys(g.Resources) {
		n := g.Resources[k]
		name, _ := nt.tsName(n.Name)
		resourceGroups[name] = append(resourceGroups[name], n)
	}
	for name, group := range resourceGroups {
		if len(group) == 1 {
			// If there is only one resource in this group, allow disambiguation to happen normally.
			nt.assignResource(group[0])
		} else {
			// Otherwise, force all resources in this group to disambiguate.
			nt.assigned[name] = true
			for _, n := range group {
				nt.assignResource(n)
			}
		}
	}

	return nt.names
}

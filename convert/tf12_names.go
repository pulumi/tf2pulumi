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

package convert

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
)

type nameTable struct {
	assigned map[string]bool
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

// isLegalIdentifierStart returns true if it is legal for c to be the first character of an HCL2 identifier.
func isLegalIdentifierStart(c rune) bool {
	return c == '$' || c == '_' ||
		unicode.In(c, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Lm, unicode.Lo, unicode.Nl)
}

// isLegalIdentifierPart returns true if it is legal for c to be part of an HCL2 identifier.
func isLegalIdentifierPart(c rune) bool {
	return isLegalIdentifierStart(c) || unicode.In(c, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc)
}

// cleanName replaces characters that are not allowed in JavaScript identifiers with underscores. No attempt is made to
// ensure that the result is unique.
func cleanName(name string) string {
	var builder strings.Builder
	for i, c := range name {
		if !isLegalIdentifierPart(c) {
			builder.WriteRune('_')
		} else {
			if i == 0 && !isLegalIdentifierStart(c) {
				builder.WriteRune('_')
			}
			builder.WriteRune(c)
		}
	}
	return builder.String()
}

// pulumiName computes the Pulumi form of the given name.
func (nt *nameTable) pulumiName(name string) string {
	return camel(tfbridge.TerraformToPulumiName(name, nil, nil, false))
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
func (nt *nameTable) assignOutput(n *output) {
	name := nt.pulumiName(n.name)
	contract.Assert(!nt.assigned[name])

	n.pulumiName, nt.assigned[name] = name, true
}

// assignLocal assigns an unambiguous name to a local node.
func (nt *nameTable) assignLocal(n *local) {
	name := nt.pulumiName(n.name)

	// If the raw name is reserved or ambiguous, first attempt to disambiguate by prepending "my".
	if nt.assigned[name] {
		name = nt.disambiguate("my" + strings.Title(name))
	}

	n.pulumiName, nt.assigned[name] = name, true
}

// assignVariable assigns an unambiguous name to a variable node.
func (nt *nameTable) assignVariable(n *variable) {
	name := nt.pulumiName(n.name)

	// If the raw name is reserved or ambiguous, first attempt to disambiguate by appending "Input".
	if nt.assigned[name] {
		name = nt.disambiguate(name + "Input")
	}

	n.pulumiName, nt.assigned[name] = name, true
}

// assignModule assigns an unambiguous name to a module node.
func (nt *nameTable) assignModule(n *module) {
	name := nt.pulumiName(n.name)

	// If the raw name is ambiguous, first attempt to disambiguate by appending "Instance"
	if nt.assigned[name] {
		name = nt.disambiguate(name + "Instance")
	}

	n.pulumiName, nt.assigned[name] = name, true
}

// assignProvider assigns an unambiguous name to a provider node.
func (nt *nameTable) assignProvider(n *provider) {
	name := nt.pulumiName(n.alias)

	// If the raw name is ambiguous, first attempt to disambiguate by prepending the package name.
	if nt.assigned[name] {
		name = nt.disambiguate(n.pluginName + title(name))
	}

	n.pulumiName, nt.assigned[name] = name, true
}

// disambiguateResourceName computes an unambiguous name for the given resource node.
func (nt *nameTable) disambiguateResourceName(n *resource) string {
	name := nt.pulumiName(n.name)

	if len(name) == 1 {
		// If the name is a single character, ignore it and fall through to the disambiguator.
		name = ""
	} else if name != "" && !nt.assigned[name] {
		// If the name is not reserved and is unambiguous, use it.
		return name
	}

	// Determine the resource's Pulumi package, module, and type name. These will be used during the disambiguation
	// process. If these names cannot be determined, return an ugly name comprised of the TF type and name.
	packageName, moduleName, typeName, diags := hcl2.DecomposeToken(n.token, hcl.Range{})
	if len(diags) != 0 {
		return cleanName(n.typeName + "_" + n.name)
	}
	packageName, moduleName = title(packageName), title(moduleName)

	// If we're dealing with a data source, strip any leading "get" from the typeName.
	if n.isDataSource && strings.HasPrefix(typeName, "get") {
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
func (nt *nameTable) assignResource(n *resource) {
	name := nt.disambiguateResourceName(n)
	n.pulumiName, nt.assigned[name] = name, true
}

func assignNames(files []*file) {
	nt := &nameTable{
		assigned: make(map[string]bool),
	}

	var outputs []*output
	var variables []*variable
	var locals []*local
	var modules []*module
	var providers []*provider
	var resources []*resource
	for _, f := range files {
		for _, n := range f.nodes {
			switch n := n.(type) {
			case *output:
				outputs = append(outputs, n)
			case *variable:
				variables = append(variables, n)
			case *local:
				locals = append(locals, n)
			case *module:
				modules = append(modules, n)
			case *provider:
				providers = append(providers, n)
			case *resource:
				resources = append(resources, n)
			}
		}
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].name < outputs[j].name })
	sort.Slice(variables, func(i, j int) bool { return variables[i].name < variables[j].name })
	sort.Slice(locals, func(i, j int) bool { return locals[i].name < locals[j].name })
	sort.Slice(modules, func(i, j int) bool { return modules[i].name < modules[j].name })
	sort.Slice(providers, func(i, j int) bool { return providers[i].alias < providers[j].alias })
	sort.Slice(resources, func(i, j int) bool { return resources[i].name < resources[j].name })

	// Assign output names first: given a conflict between nodes, we always want the output node (if any) to win so
	// that output names are predictable and as consistent with their TF names as is possible.
	for _, n := range outputs {
		nt.assignOutput(n)
	}

	// Next, record all other nodes in the following order:
	// 1. Variables
	// 2. Locals
	// 3. Modules
	// 4. Providers
	// 5. Resources
	for _, n := range variables {
		nt.assignVariable(n)
	}
	for _, n := range locals {
		nt.assignLocal(n)
	}
	for _, n := range modules {
		nt.assignModule(n)
	}
	for _, n := range providers {
		nt.assignProvider(n)
	}

	// We handle resources in two passes: in the first pass, we decide which names are ambiguous, and in the second pass
	// we assign names. We do this so that we can apply disambiguation more uniformly across resource names.
	resourceGroups := make(map[string][]*resource)
	for _, n := range resources {
		name := nt.pulumiName(n.name)
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
}

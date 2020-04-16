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

// Package python implements a Python back-end for tf2pulumi's intermediate representation. It is responsible for
// translating the Graph IR emit by the frontend into valid Pulumi Python code that is as semantically equivalent to
// the original Terraform as possible.
package python

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// New creates a new Python Generator that writes to the given writer and uses the given project name.
func New(projectName string, w io.Writer) gen.Generator {
	g := &generator{projectName: projectName}
	g.Emitter = gen.NewEmitter(w, g)
	return g
}

type generator struct {
	// The emitter to use when generating code.
	*gen.Emitter

	projectName   string
	needNYIHelper bool

	// put here because of copy-pasta
	// countIndex is the name (if any) of the currently in-scope count variable.
	countIndex string
	// unknownInputs is the set of input variables that may be unknown at runtime.
	unknownInputs map[*il.VariableNode]struct{}
}

func (g *generator) GeneratePreamble(modules []*il.Graph) error {
	g.Println("import pulumi")

	// Accumulate other imports for the various providers. Don't emit them yet, as we need to sort them later on.
	var imports []string
	providers := make(map[string]bool)
	for _, m := range modules {
		for _, p := range m.Providers {
			name := p.PluginName
			if !providers[name] {
				providers[name] = true
				switch name {
				case "archive":
					return errors.New("NYI: Python Archive Provider")
				case "http":
					return errors.New("NYI: Python HTTP Provider")
				default:
					imports = append(imports, fmt.Sprintf("import pulumi_%[1]s as %[1]s", name))
				}
			}
		}
	}

	// TODO(swgillespie) walk the graph to find optional imports

	sort.Strings(imports)
	for _, pkg := range imports {
		g.Println(pkg)
	}
	g.Println("") // end the preamble with a newline, standard Python style.
	return nil
}

func (g *generator) BeginModule(mod *il.Graph) error {
	if !mod.IsRoot {
		return errors.New("NYI: Python Modules")
	}
	return nil
}

func (g *generator) EndModule(mod *il.Graph) error {
	g.genNYIHelper(g)
	return nil
}

func (g *generator) GenerateVariables(vs []*il.VariableNode) error {
	if len(vs) != 0 {
		return errors.New("NYI: Python Variables")
	}
	return nil
}

func (g *generator) GenerateModule(m *il.ModuleNode) error {
	return errors.New("NYI: Python Modules")
}

func (g *generator) GenerateLocal(l *il.LocalNode) error {
	return errors.New("NYI: Python Locals")
}

func (g *generator) GenerateProvider(p *il.ProviderNode) error {
	if p.Alias == "" {
		return nil
	}
	return errors.New("NYI: Python Providers")
}

func (g *generator) GenerateResource(r *il.ResourceNode) error {
	pkg, subpkg, class, err := resourceTypeName(r)
	if err != nil {
		return err
	}
	if subpkg != "" {
		subpkg = "." + subpkg
	}

	// TODO(swgillespie) resource explicit dependencies
	if len(r.ExplicitDeps) != 0 {
		return errors.New("NYI: Python Explicit Dependencies")
	}

	name := g.nodeName(r)
	// Prepare the inputs by lifting them into applies, as necessary. If this is a data source, we must also lift the
	// data source call itself into the apply.
	if r.IsDataSource {
		// Qualified member names are snake cased for data sources.
		qualifiedMemberName := fmt.Sprintf("%s%s.%s", pkg, subpkg, pyName(class))
		properties := newDataSourceCall(qualifiedMemberName, r.Properties)
		inputs, transformed, err := g.computeProperty(properties, false, "")
		if err != nil {
			return err
		}

		// If computeProperty transformed the input bag, it is already output-typed; otherwise, it must be made
		// output-typed using `from_input`.
		if transformed {
			g.Printf("%s%s = %s\n", g.Indent, name, inputs)
		} else {
			g.Printf("%s%s = pulumi.Output.from_input(%s)\n", g.Indent, name, inputs)
		}
	} else {
		// For resources, the property inputs must still be apply-rewritten, but the resource invocation itself should
		// not.
		qualifiedMemberName := fmt.Sprintf("%s%s.%s", pkg, subpkg, class)
		inputs, err := g.transformProperty(r.Properties)
		if err != nil {
			return err
		}

		// Unlike the Node backend, the Python backend represents resource calls as calls to the __resource intrinsic.
		// The reason for this is that Python draws a sharp distinction between map-based inputs and top-level
		// properties of a resource: the first is typed as a dinctionary while the second is typed as a series of
		// keyword arguments to a constructor.
		//
		// hil.go is responsible for rewriting the __resource intrinsic into a call to a resource's constructor.
		resCall := newResourceCall(qualifiedMemberName, r.Name, inputs.(*il.BoundMapProperty))
		buf := &bytes.Buffer{}
		g.Fgen(buf, resCall)
		g.Printf("%s%s = %s\n", g.Indent, name, buf.String())
	}
	return nil
}

func (g *generator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) != 0 {
		return errors.New("NYI: Python Outputs")
	}
	return nil
}

// lowerToLiterals gives the generator a chance to lower certain elements into literals before code generation. It is
// unclear whether or not this is useful for Python yet.
func (g *generator) lowerToLiterals(prop il.BoundNode) (il.BoundNode, error) {
	return prop, nil
}

// nodeName returns a name suitable for the given node. It consults the IL to determine a good name for the node,
// returning the selected name.
//
// In the future, this name will need to be at least somewhat unique to avoid redefining local variables, since
// Terraform namespaces names by resource and we do not. For now, this returns the Terraform name for a particular
// resource, which may not be unique.
func (g *generator) nodeName(n il.Node) string {
	if res, ok := n.(*il.ResourceNode); ok {
		return res.Name
	}

	// Obviously not great...
	return "unknown"
}

// cleanName takes a name visible in Terraform config and translates it to a form suitable for Python. This involves
// working around keywords and other things that are otherwise not legal in Python identifiers.
func cleanName(name string) string {
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return name + "_"
	}
	return name
}

//
// Copy-pasted but modified stuff from the node backend.
//
func (g *generator) transformProperty(prop il.BoundNode) (il.BoundNode, error) {
	// First:
	// - retype any possibly-unknown module inputs as the appropriate output types
	// - discover whether or not the property contains any output-typed expressions
	containsOutputs := false
	_, err := il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if n, ok := n.(*il.BoundVariableAccess); ok {
			if v, ok := n.ILNode.(*il.VariableNode); ok {
				if _, ok = g.unknownInputs[v]; ok {
					n.ExprType = n.ExprType.OutputOf()
				}
			}
			containsOutputs = containsOutputs || n.Type().IsOutput()
		}
		return n, nil
	})
	contract.Assert(err == nil)

	// Next, rewrite assets, lower certain constructrs to literals, insert any necessary coercions, and run the apply
	// transform.
	p, err := il.RewriteAssets(prop)
	if err != nil {
		return nil, err
	}

	p, err = g.lowerToLiterals(p)
	if err != nil {
		return nil, err
	}

	p, err = il.AddCoercions(p)
	if err != nil {
		return nil, err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return nil, err
	}

	return RewriteTrivialApplies(p)
}

//
// Copy-pasted stuff from the node backend. Don't modify anything below this line!
//

// computeProperty generates code for the given property into a string ala fmt.Sprintf. It returns both the generated
// code and a bool value that indicates whether or not any output-typed values were nested in the property value.
func (g *generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, bool, error) {
	// First:
	// - retype any possibly-unknown module inputs as the appropriate output types
	// - discover whether or not the property contains any output-typed expressions
	containsOutputs := false
	_, err := il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if n, ok := n.(*il.BoundVariableAccess); ok {
			if v, ok := n.ILNode.(*il.VariableNode); ok {
				if _, ok = g.unknownInputs[v]; ok {
					n.ExprType = n.ExprType.OutputOf()
				}
			}
			containsOutputs = containsOutputs || n.Type().IsOutput()
		}
		return n, nil
	})
	contract.Assert(err == nil)

	// Next, rewrite assets, lower certain constructrs to literals, insert any necessary coercions, and run the apply
	// transform.
	p, err := il.RewriteAssets(prop)
	if err != nil {
		return "", false, err
	}

	p, err = g.lowerToLiterals(p)
	if err != nil {
		return "", false, err
	}

	p, err = il.AddCoercions(p)
	if err != nil {
		return "", false, err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return "", false, err
	}

	// Finally, generate code for the property.
	if indent {
		g.Indent += "    "
		defer func() { g.Indent = g.Indent[:len(g.Indent)-4] }()
	}
	g.countIndex = count
	buf := &bytes.Buffer{}
	g.Fgen(buf, p)
	return buf.String(), containsOutputs, nil
}

// resourceTypeName computes the Python package, subpackage, and class name for a given resource.
func resourceTypeName(r *il.ResourceNode) (string, string, string, error) {
	// Compute the resource type from the Terraform type.
	underscore := strings.IndexRune(r.Type, '_')
	if underscore == -1 {
		return "", "", "", errors.New("NYI: single-resource providers")
	}
	provider, resourceType := cleanName(r.Provider.PluginName), r.Type[underscore+1:]

	// Convert the TF resource type into its Pulumi name.
	memberName := tfbridge.TerraformToPulumiName(resourceType, nil, nil, true)

	// Compute the module in which the Pulumi type definition lives.
	module := ""
	if tok, ok := r.Tok(); ok {
		components := strings.Split(tok, ":")
		if len(components) != 3 {
			return "", "", "", errors.Errorf("unexpected resource token format %s", tok)
		}

		mod, typ := components[1], components[2]

		slash := strings.IndexRune(mod, '/')
		if slash == -1 {
			slash = len(mod)
		}

		module, memberName = mod[:slash], typ
		if module == "index" {
			module = ""
		}
	}

	return provider, module, memberName, nil
}

//
// Copy-pasted from tfgen
//

// pyName turns a variable or function name, normally using camelCase, to an underscore_case name.
func pyName(name string) string {
	// This method is a state machine with four states:
	//   stateFirst - the initial state.
	//   stateUpper - The last character we saw was an uppercase letter and the character before it
	//                was either a number or a lowercase letter.
	//   stateAcronym - The last character we saw was an uppercase letter and the character before it
	//                  was an uppercase letter.
	//   stateLowerOrNumber - The last character we saw was a lowercase letter or a number.
	//
	// The following are the state transitions of this state machine:
	//   stateFirst -> (uppercase letter) -> stateUpper
	//   stateFirst -> (lowercase letter or number) -> stateLowerOrNumber
	//      Append the lower-case form of the character to currentComponent.
	//
	//   stateUpper -> (uppercase letter) -> stateAcronym
	//   stateUpper -> (lowercase letter or number) -> stateLowerOrNumber
	//      Append the lower-case form of the character to currentComponent.
	//
	//   stateAcronym -> (uppercase letter) -> stateAcronym
	//		Append the lower-case form of the character to currentComponent.
	//   stateAcronym -> (number) -> stateLowerOrNumber
	//      Append the character to currentComponent.
	//   stateAcronym -> (lowercase letter) -> stateLowerOrNumber
	//      Take all but the last character in currentComponent, turn that into
	//      a string, and append that to components. Set currentComponent to the
	//      last two characters seen.
	//
	//   stateLowerOrNumber -> (uppercase letter) -> stateUpper
	//      Take all characters in currentComponent, turn that into a string,
	//      and append that to components. Set currentComponent to the last
	//      character seen.
	//	 stateLowerOrNumber -> (lowercase letter) -> stateLowerOrNumber
	//      Append the character to currentComponent.
	//
	// The Go libraries that convert camelCase to snake_case deviate subtly from
	// the semantics we're going for in this method, namely that they separate
	// numbers and lowercase letters. We don't want this in all cases (we want e.g. Sha256Hash to
	// be converted as sha256_hash). We also want SHA256Hash to be converted as sha256_hash, so
	// we must at least be aware of digits when in the stateAcronym state.
	//
	// As for why this is a state machine, the libraries that do this all pretty much use
	// either regular expressions or state machines, which I suppose are ultimately the same thing.
	const (
		stateFirst = iota
		stateUpper
		stateAcronym
		stateLowerOrNumber
	)

	var components []string     // The components that will be joined together with underscores
	var currentComponent []rune // The characters composing the current component being built
	state := stateFirst
	for _, char := range name {
		switch state {
		case stateFirst:
			if unicode.IsUpper(char) {
				// stateFirst -> stateUpper
				state = stateUpper
				currentComponent = append(currentComponent, unicode.ToLower(char))
				continue
			}

			// stateFirst -> stateLowerOrNumber
			state = stateLowerOrNumber
			currentComponent = append(currentComponent, char)
			continue

		case stateUpper:
			if unicode.IsUpper(char) {
				// stateUpper -> stateAcronym
				state = stateAcronym
				currentComponent = append(currentComponent, unicode.ToLower(char))
				continue
			}

			// stateUpper -> stateLowerOrNumber
			state = stateLowerOrNumber
			currentComponent = append(currentComponent, char)
			continue

		case stateAcronym:
			if unicode.IsUpper(char) {
				// stateAcronym -> stateAcronym
				currentComponent = append(currentComponent, unicode.ToLower(char))
				continue
			}

			// We want to fold digits immediately following an acronym into the same
			// component as the acronym.
			if unicode.IsDigit(char) {
				// stateAcronym -> stateLowerOrNumber
				currentComponent = append(currentComponent, char)
				state = stateLowerOrNumber
				continue
			}

			// stateAcronym -> stateLowerOrNumber
			last, rest := currentComponent[len(currentComponent)-1], currentComponent[:len(currentComponent)-1]
			components = append(components, string(rest))
			currentComponent = []rune{last, char}
			state = stateLowerOrNumber
			continue

		case stateLowerOrNumber:
			if unicode.IsUpper(char) {
				// stateLowerOrNumber -> stateUpper
				components = append(components, string(currentComponent))
				currentComponent = []rune{unicode.ToLower(char)}
				state = stateUpper
				continue
			}

			// stateLowerOrNumber -> stateLowerOrNumber
			currentComponent = append(currentComponent, char)
			continue
		}
	}

	components = append(components, string(currentComponent))
	result := strings.Join(components, "_")
	return ensurePythonKeywordSafe(result)
}

// pythonKeywords is a map of reserved keywords used by Python 2 and 3.  We use this to avoid generating unspeakable
// names in the resulting code.  This map was sourced by merging the following reference material:
//
//     * Python 2: https://docs.python.org/2.5/ref/keywords.html
//     * Python 3: https://docs.python.org/3/reference/lexical_analysis.html#keywords
//
var pythonKeywords = map[string]bool{
	"False":    true,
	"None":     true,
	"True":     true,
	"and":      true,
	"as":       true,
	"assert":   true,
	"async":    true,
	"await":    true,
	"break":    true,
	"class":    true,
	"continue": true,
	"def":      true,
	"del":      true,
	"elif":     true,
	"else":     true,
	"except":   true,
	"exec":     true,
	"finally":  true,
	"for":      true,
	"from":     true,
	"global":   true,
	"if":       true,
	"import":   true,
	"in":       true,
	"is":       true,
	"lambda":   true,
	"nonlocal": true,
	"not":      true,
	"or":       true,
	"pass":     true,
	"print":    true,
	"raise":    true,
	"return":   true,
	"try":      true,
	"while":    true,
	"with":     true,
	"yield":    true,
}

// ensurePythonKeywordSafe adds a trailing underscore if the generated name clashes with a Python 2 or 3 keyword, per
// PEP 8: https://www.python.org/dev/peps/pep-0008/?#function-and-method-arguments
func ensurePythonKeywordSafe(name string) string {
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return name + "_"
	}
	return name
}

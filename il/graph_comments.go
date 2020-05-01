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
	"path"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/hcl/hcl/token"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/spf13/afero"

	"github.com/pulumi/tf2pulumi/internal/config"
)

// locatable is an interface shared by the IL's top-level nodes.
type locatable interface {
	GetLocation() token.Pos
	setLocation(p token.Pos)
}

// commentable is an interface shared by the IL's top-level nodes (e.g. ResourceNode, OutputNode) and its bound
// nodes.
type commentable interface {
	setComments(c *Comments)
}

// extractComments annotates the builder's nodes with comments extracted from the given config's HCL sources. This
// is a best-effort process: not all comments are extractable due to weaknesses in the HCL parser. This process will
// only fail if files are unreadable or unparseable.
func (b *builder) extractComments(c *config.Config) error {
	// Nothing we can do if `Dir` is empty.
	if c.Dir == "" {
		return nil
	}

	// Find all config and/or override files in the directory.
	files, err := afero.ReadDir(c.Fs, c.Dir)
	if err != nil {
		return err
	}
	var configs, overrides []string
	for _, f := range files {
		if f.IsDir() || config.IsIgnoredFile(f.Name()) || !strings.HasSuffix(f.Name(), ".tf") {
			continue
		}

		// Check to see if the file is an override.
		if n := f.Name()[:len(f.Name())-len(".tf")]; n == "override" || strings.HasSuffix(n, "_override") {
			overrides = append(overrides, f.Name())
		} else {
			configs = append(configs, f.Name())
		}
	}

	for _, f := range configs {
		if err := b.extractFileComments(c.Fs, path.Join(c.Dir, f)); err != nil {
			return err
		}
	}
	for _, f := range overrides {
		if err := b.extractFileComments(c.Fs, path.Join(c.Dir, f)); err != nil {
			return err
		}
	}
	return nil
}

// extractFileComments extracts comments from a particular HCL source file.
func (b *builder) extractFileComments(fs afero.Fs, filePath string) error {
	t, err := afero.ReadFile(fs, filePath)
	if err != nil {
		return err
	}

	f, err := hcl.ParseBytes(t)
	if err != nil {
		return err
	}

	b.extractHCLComments(f, path.Base(filePath))
	return nil
}

// extractHCLComments extracts comments from the given HCL file.
func (b *builder) extractHCLComments(f *ast.File, path string) {
	root, ok := f.Node.(*ast.ObjectList)
	if !ok {
		b.logf("unexpected type for HCL root node '%T'; skipping file...", f.Node)
		return
	}

	// Extract variable comments
	for _, n := range root.Items {
		switch n.Keys[0].Token.Value().(string) {
		case "variable":
			b.extractVariableComments(n, path)
		case "provider":
			b.extractProviderComments(n, path)
		case "module":
			b.extractModuleComments(n, path)
		case "resource":
			b.extractResourceComments(n, path, config.ManagedResourceMode)
		case "data":
			b.extractResourceComments(n, path, config.DataResourceMode)
		case "locals":
			if object, ok := n.Val.(*ast.ObjectType); ok {
				for _, ln := range object.List.Items {
					b.extractLocalComments(ln, path)
				}
			} else {
				b.logf("unexpected locals type '%T'; skipping node...", n.Val)
			}
		case "output":
			b.extractOutputComments(n, path)
		}
	}
}

// extractVariableComments extracts comments from the given HCL object item and attaches them to the corresponding
// variable node, if any exists.
func (b *builder) extractVariableComments(item *ast.ObjectItem, path string) {
	name := item.Keys[1].Token.Value().(string)
	v, ok := b.variables[name]
	if !ok {
		return
	}

	attachLocation(v, item.Pos(), path)
	attachComments(v, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, &BoundMapProperty{Elements: map[string]BoundNode{"default": v.DefaultValue}})
}

// extractProviderComments extracts comments from the given HCL object item and attaches them to the corresponding
// provider node, if any exists.
func (b *builder) extractProviderComments(item *ast.ObjectItem, path string) {
	// We need the provider's alias in order to look up its node. This requires parsing the body of the item.
	object, ok := item.Val.(*ast.ObjectType)
	if !ok {
		return
	}

	alias := ""
	for _, property := range object.List.Items {
		key, ok := property.Keys[0].Token.Value().(string)
		if ok && key == "alias" {
			if aliasLiteral, ok := property.Val.(*ast.LiteralType); ok {
				alias, _ = aliasLiteral.Token.Value().(string)
			}
		}
	}

	name := item.Keys[1].Token.Value().(string)
	p, ok := b.providers[(&config.ProviderConfig{Name: name, Alias: alias}).FullName()]
	if !ok {
		return
	}

	attachLocation(p, item.Pos(), path)
	attachComments(p, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, p.Properties)
}

// extractModuleComments extracts comments from the given HCL object item and attaches them to the corresponding
// module node, if any exists.
func (b *builder) extractModuleComments(item *ast.ObjectItem, path string) {
	name := item.Keys[1].Token.Value().(string)
	m, ok := b.modules[name]
	if !ok {
		return
	}

	attachLocation(m, item.Pos(), path)
	attachComments(m, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, m.Properties)
}

// extractResourceComments extracts comments from the given HCL object item and attaches them to the corresponding
// resource node, if any exists.
func (b *builder) extractResourceComments(item *ast.ObjectItem, path string, mode config.ResourceMode) {
	cfg := &config.Resource{
		Mode: mode,
		Name: item.Keys[2].Token.Value().(string),
		Type: item.Keys[1].Token.Value().(string),
	}
	r, ok := b.resources[cfg.Id()]
	if !ok {
		return
	}

	attachLocation(r, item.Pos(), path)
	attachComments(r, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, r.Properties)
}

// extractLocalComments extracts comments from the given HCL object item and attaches them to the corresponding
// local node, if any exists.
func (b *builder) extractLocalComments(item *ast.ObjectItem, path string) {
	name := item.Keys[0].Token.Value().(string)
	l, ok := b.locals[name]
	if !ok {
		return
	}

	attachLocation(l, item.Pos(), path)
	attachComments(l, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, l.Value)
}

// extractOutputComments extracts comments from the given HCL object item and attaches them to the corresponding
// output node, if any exists.
func (b *builder) extractOutputComments(item *ast.ObjectItem, path string) {
	name := item.Keys[1].Token.Value().(string)
	o, ok := b.outputs[name]
	if !ok {
		return
	}

	attachLocation(o, item.Pos(), path)
	attachComments(o, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, &BoundMapProperty{Elements: map[string]BoundNode{"value": o.Value}})
}

// extractNodeComments recursively extracts comments from the given HCL AST node and attaches them to the appropriate
// piece of the given context. Currently this only operates on list and object nodes and their elements.
func (b *builder) extractNodeComments(node ast.Node, context BoundNode) {
	switch node := node.(type) {
	case *ast.ListType:
		prop, ok := context.(*BoundListProperty)
		if !ok {
			return
		}
		for i, item := range node.List {
			element := prop.Elements[i]
			if literal, ok := item.(*ast.LiteralType); ok {
				attachComments(element, literal.LeadComment, literal.LineComment)
			} else {
				b.extractNodeComments(item, element)
			}
		}
	case *ast.ObjectType:
		// TF does some very strange things when it comes to wrapping objects in lists.
		if list, ok := context.(*BoundListProperty); ok && len(list.Elements) == 1 {
			context = list.Elements[0]
		}
		prop, ok := context.(*BoundMapProperty)
		if !ok {
			return
		}

		objectItems := make(map[string][]*ast.ObjectItem)
		for _, item := range node.List.Items {
			key := item.Keys[0].Token.Value().(string)
			objectItems[key] = append(objectItems[key], item)
		}

		for key, items := range objectItems {
			element, ok := prop.Elements[key]
			if !ok {
				continue
			}

			if len(items) == 1 {
				// If there is only one item for a key, we associate its comments with the element itself.
				item := items[0]
				attachComments(element, item.LeadComment, item.LineComment)
				b.extractNodeComments(item.Val, element)
			} else if list, ok := element.(*BoundListProperty); ok && len(items) == len(list.Elements) {
				// If there are mutiple items for a key and they correspond to a list property, attempt to associate
				// each item's comments with its corresponding list element.
				for i, item := range items {
					element = list.Elements[i]
					attachComments(element, item.LeadComment, item.LineComment)
					b.extractNodeComments(item.Val, element)
				}
			} else {
				// This is a strange case: we have multiple items with the same key in the object, but the
				// corresponding property is not a list or differs in length. Log it and carry on.
				b.logf("list mismatch for key '%v': %v, %T", key, len(items), element)
			}
		}
	case *ast.LiteralType:
		// We only encounter this case when recursing on the value associated with an object item. In this case, the
		// literal itself has no associated comments, as they are stored on the object item.
	default:
		b.logf("unhandled ast type %T (%v)", node, node)
	}
}

// attachLocation attaches the indicated location to a node and sets the file path appropriately.
func attachLocation(n locatable, pos token.Pos, path string) {
	pos.Filename = path
	n.setLocation(pos)
}

// attachComments preprocesses the given leading and trailing comments (if any) and attaches them to the given node.
func attachComments(n commentable, leading, trailing *ast.CommentGroup) {
	c := Comments{
		Leading:  extractComment(leading),
		Trailing: extractComment(trailing),
	}
	if len(c.Leading) != 0 || len(c.Trailing) != 0 {
		n.setComments(&c)
	}
}

// extractComment separates the given comment into lines and attempts to remove comment tokens.
func extractComment(g *ast.CommentGroup) []string {
	if g == nil {
		return nil
	}

	// An ast.CommentGroup is composed of a list of adjacent comments in the order in which they appeared in the
	// source.
	//
	// Each HCL comment may be either a line comment or a block comment. Line comments start with '#' or '//' and
	// terminate with an EOL. Block comments begin with a '/*' and terminate with a '*/'. All comment delimiters are
	// preserved in the HCL comment text.
	//
	// To make life easier for the code generators, HCL comments are pre-processed to remove comment delimiters. For
	// line comments, this process is trivial. For block comments, things are a bit more involved.
	var lines []string
	for _, c := range g.List {
		comment := c.Text
		switch {
		case comment[0] == '#':
			lines = append(lines, comment[1:])
		case comment[0:2] == "//":
			lines = append(lines, comment[2:])
		default:
			lines = append(lines, processBlockComment(comment)...)
		}
	}
	return lines
}

// These regexes are used by processBlockComment. The first matches a block comment start, the second a block comment
// end, and the third a block comment line prefix.
var blockStartPat = regexp.MustCompile(`^/\*+`)
var blockEndPat = regexp.MustCompile(`[[:space:]]*\*+/$`)
var blockPrefixPat = regexp.MustCompile(`^[[:space:]]*\*`)

// processBlockComment splits a block comment into mutiple lines, removes comment delimiters, and attempts to remove
// common comment prefixes from interior lines. For example, the following HCL block comment:
//
//     /**
//      * This is a block comment!
//      *
//      * It has multiple lines.
//      */
//
// becomes this set of lines:
//
//     []string{" This is a block comment!", "", " It has multiple lines."}
//
func processBlockComment(text string) []string {
	lines := strings.Split(text, "\n")

	// We will always trim the following:
	// - '^/\*+' from the first line
	// - a trailing '[[:space:]]\*+/$' from the last line

	// In addition, we will trim '^[[:space:]]*\*' from the second through last lines iff all lines in that set share
	// a prefix that matches that pattern.

	prefix := ""
	for i, l := range lines[1:] {
		m := blockPrefixPat.FindString(l)
		if i == 0 {
			prefix = m
		} else if m != prefix {
			prefix = ""
			break
		}
	}

	for i, l := range lines {
		switch i {
		case 0:
			start := blockStartPat.FindString(l)
			contract.Assert(start != "")
			l = l[len(start):]

			// If this is a single-line block comment, trim the end pattern as well.
			if len(lines) == 1 {
				contract.Assert(prefix == "")

				end := blockEndPat.FindString(l)
				contract.Assert(end != "")
				l = l[:len(l)-len(end)]
			}
		case len(lines) - 1:
			// The prefix we're trimming may overlap with the end pattern we're trimming. In this case, trim the entire
			// line.
			if len(l)-len(prefix) == 1 {
				l = ""
			} else {
				l = l[len(prefix):]
				end := blockEndPat.FindString(l)
				contract.Assert(end != "")
				l = l[:len(l)-len(end)]
			}
		default:
			// Trim the prefix.
			l = l[len(prefix):]
		}

		lines[i] = l
	}

	// If the first or last line is empty, drop it.
	if lines[0] == "" {
		lines = lines[1:]
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

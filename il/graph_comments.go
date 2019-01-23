package il

import (
	"io/ioutil"
	"path"
	"strings"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/terraform/config"
)

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
	files, err := ioutil.ReadDir(c.Dir)
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
		if err := b.extractFileComments(path.Join(c.Dir, f)); err != nil {
			return err
		}
	}
	for _, f := range overrides {
		if err := b.extractFileComments(path.Join(c.Dir, f)); err != nil {
			return err
		}
	}
	return nil
}

// extractFileComments extracts comments from a particular HCL source file.
func (b *builder) extractFileComments(path string) error {
	t, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	f, err := hcl.ParseBytes(t)
	if err != nil {
		return err
	}

	b.extractHCLComments(f)
	return nil
}

// extractHCLComments extracts comments from the given HCL file.
func (b *builder) extractHCLComments(f *ast.File) {
	root, ok := f.Node.(*ast.ObjectList)
	if !ok {
		return
	}

	// Extract variable comments
	for _, n := range root.Items {
		switch n.Keys[0].Token.Value().(string) {
		case "variable":
			b.extractVariableComments(n)
		case "provider":
			b.extractProviderComments(n)
		case "module":
			b.extractModuleComments(n)
		case "resource":
			b.extractResourceComments(n, config.ManagedResourceMode)
		case "data":
			b.extractResourceComments(n, config.DataResourceMode)
		case "locals":
			if object, ok := n.Val.(*ast.ObjectType); ok {
				for _, ln := range object.List.Items {
					b.extractLocalComments(ln)
				}
			}
		case "output":
			b.extractOutputComments(n)
		}
	}
}

// extractVariableComments extracts comments from the given HCL object item and attaches them to the corresponding
// variable node, if any exists.
func (b *builder) extractVariableComments(item *ast.ObjectItem) {
	name := item.Keys[1].Token.Value().(string)
	v, ok := b.variables[name]
	if !ok {
		return
	}

	attachComments(v, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, &BoundMapProperty{Elements: map[string]BoundNode{"default": v.DefaultValue}})
}

// extractProviderComments extracts comments from the given HCL object item and attaches them to the corresponding
// provider node, if any exists.
func (b *builder) extractProviderComments(item *ast.ObjectItem) {
	name := item.Keys[1].Token.Value().(string)
	p, ok := b.providers[name]
	if !ok {
		return
	}

	attachComments(p, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, p.Properties)
}

// extractModuleComments extracts comments from the given HCL object item and attaches them to the corresponding
// module node, if any exists.
func (b *builder) extractModuleComments(item *ast.ObjectItem) {
	name := item.Keys[1].Token.Value().(string)
	m, ok := b.modules[name]
	if !ok {
		return
	}

	attachComments(m, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, m.Properties)
}

// extractResourceComments extracts comments from the given HCL object item and attaches them to the corresponding
// resource node, if any exists.
func (b *builder) extractResourceComments(item *ast.ObjectItem, mode config.ResourceMode) {
	cfg := &config.Resource{
		Mode: mode,
		Name: item.Keys[2].Token.Value().(string),
		Type: item.Keys[1].Token.Value().(string),
	}
	r, ok := b.resources[cfg.Id()]
	if !ok {
		return
	}

	attachComments(r, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, r.Properties)
}

// extractLocalComments extracts comments from the given HCL object item and attaches them to the corresponding
// local node, if any exists.
func (b *builder) extractLocalComments(item *ast.ObjectItem) {
	name := item.Keys[0].Token.Value().(string)
	l, ok := b.locals[name]
	if !ok {
		return
	}

	attachComments(l, item.LeadComment, item.LineComment)
	b.extractNodeComments(item.Val, l.Value)
}

// extractOutputComments extracts comments from the given HCL object item and attaches them to the corresponding
// output node, if any exists.
func (b *builder) extractOutputComments(item *ast.ObjectItem) {
	name := item.Keys[1].Token.Value().(string)
	o, ok := b.outputs[name]
	if !ok {
		return
	}

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

	var lines []string
	for _, c := range g.List {
		// Strip leading comment tokens.
		comment := c.Text
		switch {
		case comment[0] == '#':
			comment = comment[1:]
		case comment[0:2] == "//":
			comment = comment[2:]
		case comment[0:2] == "/*":
			// This is more complex, stripping these characters can often lead to strange alignment, and these comments
			// are relatively rare, so we leave them as-is.
		}
		lines = append(lines, strings.Split(comment, "\n")...)
	}
	return lines
}

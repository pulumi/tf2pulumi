package nodejs

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/il"
)

func (g *Generator) computeArchiveInputs(r *il.ResourceNode, indent, count string) (string, error) {
	contract.Require(r.Provider.Config.Name == "archive", "r")

	buf := &bytes.Buffer{}
	buf.WriteString("{\n")
	if sourceFile, ok := r.Properties.Elements["source_file"]; ok {
		path, err := g.computeProperty(sourceFile, "", count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.FileAsset(%s),\n", indent, path, path)
	} else if sourceDir, ok := r.Properties.Elements["source_dir"]; ok {
		path, err := g.computeProperty(sourceDir, "", count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.FileAsset(%s),\n", indent, path, path)
	} else if sourceContent, ok := r.Properties.Elements["source_content"]; ok {
		filename, ok := r.Properties.Elements["source_filename"]
		if !ok {
			return "", errors.Errorf("missing source_filename property in archive %s", r.Config.Id())
		}

		path, err := g.computeProperty(filename, "", count)
		if err != nil {
			return "", err
		}
		content, err := g.computeProperty(sourceContent, "", count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.StringAsset(%s),\n", indent, path, content)
	} else if source, ok := r.Properties.Elements["source"]; ok {
		list, ok := source.(*il.BoundListProperty)
		if !ok {
			return "", errors.Errorf("unexpected type for source in archive %s", r.Config.Id())
		}

		for _, e := range list.Elements {
			m, ok := e.(*il.BoundMapProperty)
			if !ok {
				return "", errors.Errorf("unexpected type for source in archive %s", r.Config.Id())
			}

			sourceContent, ok := m.Elements["content"]
			if !ok {
				return "", errors.Errorf("missing property \"content\" in archive %s", r.Config.Id())
			}
			sourceFilename, ok := m.Elements["filename"]
			if !ok {
				return "", errors.Errorf("missing property \"filename\" in archive %s", r.Config.Id())
			}

			content, err := g.computeProperty(sourceContent, "", count)
			if err != nil {
				return "", err
			}
			path, err := g.computeProperty(sourceFilename, "", count)
			if err != nil {
				return "", err
			}

			fmt.Fprintf(buf, "%s    %s: new pulumi.asset.StringAsset(%s),\n", indent, path, content)
		}
	}
	fmt.Fprintf(buf, "%s}", indent)
	return buf.String(), nil
}

func (g *Generator) generateArchive(r *il.ResourceNode) error {
	contract.Require(r.Provider.Config.Name == "archive", "r")

	// TODO: explicit dependencies (or any dependencies at all, really)

	name := resName(r.Config.Type, r.Config.Name)

	if r.Count == nil {
		inputs, err := g.computeArchiveInputs(r, "", "")
		if err != nil {
			return err
		}

		// Generate an asset archive.
		fmt.Printf("const %s = new pulumi.asset.AssetArchive(%s);\n", name, inputs)
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, err := g.computeProperty(r.Count, "", "")
		if err != nil {
			return err
		}
		inputs, err := g.computeArchiveInputs(r, "    ", "i")
		if err != nil {
			return err
		}

		fmt.Printf("const %s: pulumi.asset.AssetArchive[] = [];\n", name)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		fmt.Printf("    %s.push(new pulumi.asset.AssetArchive(%s));\n", name, inputs)
		fmt.Printf("}\n")
	}

	return nil
}


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
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

// computeArchiveInputs computes the inputs for a call to the pulumi.AssetArchive constructor based on the values
// present in the given resource's bound input properties.
func (g *generator) computeArchiveInputs(r *il.ResourceNode, indent bool, count string) (string, error) {
	contract.Require(r.Provider.Name == "archive", "r")

	buf := &bytes.Buffer{}
	buf.WriteString("{\n")
	if sourceFile, ok := r.Properties.Elements["source_file"]; ok {
		path, _, err := g.computeProperty(sourceFile, indent, count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.FileAsset(%s),\n", g.Indent, path, path)
	} else if sourceDir, ok := r.Properties.Elements["source_dir"]; ok {
		path, _, err := g.computeProperty(sourceDir, indent, count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.FileAsset(%s),\n", g.Indent, path, path)
	} else if sourceContent, ok := r.Properties.Elements["source_content"]; ok {
		filename, ok := r.Properties.Elements["source_filename"]
		if !ok {
			return "", errors.Errorf("missing source_filename property in archive %s", r.Name)
		}

		path, _, err := g.computeProperty(filename, indent, count)
		if err != nil {
			return "", err
		}
		content, _, err := g.computeProperty(sourceContent, indent, count)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(buf, "%s    %s: new pulumi.asset.StringAsset(%s),\n", g.Indent, path, content)
	} else if source, ok := r.Properties.Elements["source"]; ok {
		list, ok := source.(*il.BoundListProperty)
		if !ok {
			return "", errors.Errorf("unexpected type for source in archive %s", r.Name)
		}

		for _, e := range list.Elements {
			m, ok := e.(*il.BoundMapProperty)
			if !ok {
				return "", errors.Errorf("unexpected type for source in archive %s", r.Name)
			}

			sourceContent, ok := m.Elements["content"]
			if !ok {
				return "", errors.Errorf("missing property \"content\" in archive %s", r.Name)
			}
			sourceFilename, ok := m.Elements["filename"]
			if !ok {
				return "", errors.Errorf("missing property \"filename\" in archive %s", r.Name)
			}

			content, _, err := g.computeProperty(sourceContent, indent, count)
			if err != nil {
				return "", err
			}
			path, _, err := g.computeProperty(sourceFilename, indent, count)
			if err != nil {
				return "", err
			}

			fmt.Fprintf(buf, "%s    %s: new pulumi.asset.StringAsset(%s),\n", g.Indent, path, content)
		}
	}
	fmt.Fprintf(buf, "%s}", g.Indent)
	return buf.String(), nil
}

// generateArchive generates a call to the pulumi.AssetArchive constructor for the given archive resource.
func (g *generator) generateArchive(r *il.ResourceNode) error {
	contract.Require(r.Provider.Name == "archive", "r")

	// TODO: explicit dependencies (or any dependencies at all, really)

	name := g.nodeName(r)

	if r.Count == nil {
		inputs, err := g.computeArchiveInputs(r, false, "")
		if err != nil {
			return err
		}

		// Generate an asset archive.
		g.Printf("const %s = new pulumi.asset.AssetArchive(%s);", name, inputs)
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, _, err := g.computeProperty(r.Count, false, "")
		if err != nil {
			return err
		}
		inputs, err := g.computeArchiveInputs(r, true, "i")
		if err != nil {
			return err
		}

		g.Printf("const %s: pulumi.asset.AssetArchive[] = [];\n", name)
		g.Printf("for (let i = 0; i < %s; i++) {\n", count)
		g.Printf("    %s.push(new pulumi.asset.AssetArchive(%s));\n", name, inputs)
		g.Printf("}")
	}

	return nil
}

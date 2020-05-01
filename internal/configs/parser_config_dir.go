package configs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
)

// ConfigDirFiles returns lists of the primary and override files configuration
// files in the given directory.
//
// If the given directory does not exist or cannot be read, error diagnostics
// are returned. If errors are returned, the resulting lists may be incomplete.
func (p Parser) ConfigDirFiles(dir string) (primary, override []string, diags hcl.Diagnostics) {
	return p.dirFiles(dir)
}

func (p *Parser) dirFiles(dir string) (primary, override []string, diags hcl.Diagnostics) {
	infos, err := p.fs.ReadDir(dir)
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to read module directory",
			Detail:   fmt.Sprintf("Module directory %s does not exist or cannot be read.", dir),
		})
		return
	}

	for _, info := range infos {
		if info.IsDir() {
			// We only care about files
			continue
		}

		name := info.Name()
		ext := fileExt(name)
		if ext == "" || IsIgnoredFile(name) {
			continue
		}

		baseName := name[:len(name)-len(ext)] // strip extension
		isOverride := baseName == "override" || strings.HasSuffix(baseName, "_override")

		fullPath := filepath.Join(dir, name)
		if isOverride {
			override = append(override, fullPath)
		} else {
			primary = append(primary, fullPath)
		}
	}

	return
}

// fileExt returns the Terraform configuration extension of the given
// path, or a blank string if it is not a recognized extension.
func fileExt(path string) string {
	if strings.HasSuffix(path, ".tf") {
		return ".tf"
	} else if strings.HasSuffix(path, ".tf.json") {
		return ".tf.json"
	} else {
		return ""
	}
}

// IsIgnoredFile returns true if the given filename (which must not have a
// directory path ahead of it) should be ignored as e.g. an editor swap file.
func IsIgnoredFile(name string) bool {
	return strings.HasPrefix(name, ".") || // Unix-like hidden files
		strings.HasSuffix(name, "~") || // vim
		strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#") // emacs
}

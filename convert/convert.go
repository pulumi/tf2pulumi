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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi/pkg/codegen/hcl2"
	"github.com/pulumi/pulumi/pkg/codegen/hcl2/syntax"
	"github.com/pulumi/pulumi/pkg/codegen/nodejs"
	"github.com/pulumi/pulumi/pkg/codegen/python"
	"github.com/pulumi/pulumi/sdk/go/common/util/contract"
	"github.com/pulumi/tf2pulumi/il"
)

const (
	LanguageTypescript string = "typescript"
	LanguagePython     string = "python"
)

var (
	ValidLanguages = [...]string{LanguageTypescript, LanguagePython}
)

type Diagnostics struct {
	All     hcl.Diagnostics
	program *hcl2.Program
}

func (d *Diagnostics) NewDiagnosticWriter(w io.Writer, width uint, color bool) hcl.DiagnosticWriter {
	return d.program.NewDiagnosticWriter(w, width, color)
}

// Convert converts a Terraform module at the provided location into a Pulumi module, written to stdout.
func Convert(opts Options) Diagnostics {
	// Set default options where appropriate.
	if opts.Path == "" {
		opts.Path = "."
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	var files []*syntax.File
	var diagnostics hcl.Diagnostics

	// Attempt to load the config as TF11 first. If this succeeds, use TF11 semantics unless either the config
	// or the options specify otherwise.
	tf12Files, tf11Err := convertTF11(opts.Path, opts)
	if tf11Err == nil {
		// Parse the config.
		parser := syntax.NewParser()
		for filename, contents := range tf12Files {
			err := parser.ParseFile(bytes.NewReader(contents), filename)
			contract.Assert(err == nil)
		}
		files, diagnostics = parser.Files, append(diagnostics, parser.Diagnostics...)
	} else {
		tf12Files, diags := parseTF12(opts.Path, opts)
		if !diags.HasErrors() {
			files, diagnostics = tf12Files, diagnostics
		} else {
			diagnostics = append(diagnostics, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  tf11Err.Error(),
			})
		}
	}

	program, programDiags := convertTF12(files, opts)
	diagnostics = append(diagnostics, programDiags...)

	if diagnostics.HasErrors() {
		return Diagnostics{All: diagnostics, program: program}
	}

	var generatedFiles map[string][]byte
	switch opts.TargetLanguage {
	case LanguageTypescript:
		tsFiles, genDiags, _ := nodejs.GenerateProgram(program)
		generatedFiles, diagnostics = tsFiles, append(diagnostics, genDiags...)
	case LanguagePython:
		pyFiles, genDiags, _ := python.GenerateProgram(program)
		generatedFiles, diagnostics = pyFiles, append(diagnostics, genDiags...)
	}

	if diagnostics.HasErrors() {
		return Diagnostics{All: diagnostics, program: program}
	}

	for filename, contents := range generatedFiles {
		path := filepath.Join(opts.Path, filename)
		if err := ioutil.WriteFile(path, contents, 0600); err != nil {
			diagnostics = append(diagnostics, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("failed to write %v: %v", path, err),
			})
		}
	}

	return Diagnostics{All: diagnostics, program: program}
}

type Options struct {
	// AllowMissingProviders, if true, allows code-gen to continue even if resource providers are missing.
	AllowMissingProviders bool
	// AllowMissingVariables, if true, allows code-gen to continue even if the input configuration references missing
	// variables.
	AllowMissingVariables bool
	// AllowMissingComments allows binding to succeed even if there are errors extracting comments from the source.
	AllowMissingComments bool
	// AnnotateNodesWithLocations is true if the generated source code should contain comments that annotate top-level
	// nodes with their original source locations.
	AnnotateNodesWithLocations bool
	// FilterResourceNames, if true, removes the property indicated by ResourceNameProperty from all resources in the
	// graph.
	FilterResourceNames bool
	// ResourceNameProperty sets the key of the resource name property that will be removed if FilterResourceNames is
	// true.
	ResourceNameProperty string
	// Path, when set, overrides the default path (".") to load the source Terraform module from.
	Path string
	// Writer can be set to override the default behavior of writing the resulting code to stdout.
	Writer io.Writer
	// Optional source for provider schema information.
	ProviderInfoSource il.ProviderInfoSource
	// Optional logger for diagnostic information.
	Logger *log.Logger
	// The target language.
	TargetLanguage string
	// The target SDK version.
	TargetSDKVersion string
	// The TF version (TODO(pdg): auto-detect this)
	TerraformVersion string

	// TargetOptions captures any target-specific options.
	TargetOptions interface{}
}

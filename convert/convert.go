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
	"io"
	"log"

	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi/pkg/v2/codegen/hcl2/syntax"
	hcl2nodejs "github.com/pulumi/pulumi/pkg/v2/codegen/nodejs"
	hcl2python "github.com/pulumi/pulumi/pkg/v2/codegen/python"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

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
	All   hcl.Diagnostics
	files []*syntax.File
}

func (d *Diagnostics) NewDiagnosticWriter(w io.Writer, width uint, color bool) hcl.DiagnosticWriter {
	return syntax.NewDiagnosticWriter(w, d.files, width, color)
}

// Convert converts a Terraform module at the provided location into a Pulumi module, written to stdout.
func Convert(opts Options) (map[string][]byte, Diagnostics) {
	// Set default options where appropriate.
	if opts.Path == "" {
		opts.Path = "."
	}

	// Attempt to load the config as TF11 first. If this succeeds, use TF11 semantics unless either the config
	// or the options specify otherwise.
	generatedFiles, useTF12, tf11Err := convertTF11(opts.Path, opts)
	if !useTF12 {
		if tf11Err != nil {
			return nil, Diagnostics{All: hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  tf11Err.Error(),
			}}}
		}
		return generatedFiles, Diagnostics{}
	}

	var tf12Files []*syntax.File
	var diagnostics hcl.Diagnostics

	if tf11Err == nil {
		// Parse the config.
		parser := syntax.NewParser()
		for filename, contents := range generatedFiles {
			err := parser.ParseFile(bytes.NewReader(contents), filename)
			contract.Assert(err == nil)
		}
		if parser.Diagnostics.HasErrors() {
			return nil, Diagnostics{All: diagnostics, files: parser.Files}
		}
		tf12Files, diagnostics = parser.Files, append(diagnostics, parser.Diagnostics...)
	} else {
		files, diags := parseTF12(opts.Path, opts)
		if !diags.HasErrors() {
			tf12Files, diagnostics = files, append(diagnostics, diags...)
		} else {
			diagnostics = append(diagnostics, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  tf11Err.Error(),
			})
			return nil, Diagnostics{All: diagnostics}
		}
	}

	program, programDiags := convertTF12(tf12Files, opts)
	diagnostics = append(diagnostics, programDiags...)

	if diagnostics.HasErrors() {
		return nil, Diagnostics{All: diagnostics, files: tf12Files}
	}

	switch opts.TargetLanguage {
	case LanguageTypescript:
		tsFiles, genDiags, _ := hcl2nodejs.GenerateProgram(program)
		generatedFiles, diagnostics = tsFiles, append(diagnostics, genDiags...)
	case LanguagePython:
		pyFiles, genDiags, _ := hcl2python.GenerateProgram(program)
		generatedFiles, diagnostics = pyFiles, append(diagnostics, genDiags...)
	}

	if diagnostics.HasErrors() {
		return nil, Diagnostics{All: diagnostics, files: tf12Files}
	}

	return generatedFiles, Diagnostics{All: diagnostics, files: tf12Files}
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
	// Optional source for provider schema information.
	ProviderInfoSource il.ProviderInfoSource
	// Optional logger for diagnostic information.
	Logger *log.Logger
	// The target language.
	TargetLanguage string
	// The target SDK version.
	TargetSDKVersion string
	// The version of Terraform targeteds by the input configuration.
	TerraformVersion string

	// TargetOptions captures any target-specific options.
	TargetOptions interface{}
}

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

package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tf2pulumi/convert"
	"github.com/spf13/cobra"

	"github.com/pulumi/tf2pulumi/version"
)

func main() {
	var opts convert.Options
	resourceNameProperty, filterAutoNames, tarout := "", false, false

	rootCmd := &cobra.Command{
		Use:   "tf2pulumi",
		Short: "tf2pulumi converts Terraform configuration to a Pulumi TypeScript program",
		Long: `A converter that takes Terraform configuration as input and produces a
Pulumi TypeScript program that describes the same resource graph.`,

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(cmd *cobra.Command, args []string) error {
			if resourceNameProperty != "" && filterAutoNames {
				return errors.New(
					"exactly one of --filter-resource-names or --filter-auto-names may be specified")
			}

			opts.FilterResourceNames = resourceNameProperty != "" || filterAutoNames
			opts.ResourceNameProperty = resourceNameProperty

			files, diags, err := convert.Convert(opts)
			if err != nil {
				return err
			}
			if len(diags.All) > 0 {
				if err := diags.NewDiagnosticWriter(os.Stderr, 0, true).WriteDiagnostics(diags.All); err != nil {
					return err
				}
			}

			if tarout {
				w := tar.NewWriter(os.Stdout)
				for filename, contents := range files {
					if err := w.WriteHeader(&tar.Header{
						Name: filename,
						Mode: 0600,
					}); err != nil {
						return err
					}
					if _, err := w.Write(contents); err != nil {
						return err
					}
				}
				return nil
			}

			for filename, contents := range files {
				if err := ioutil.WriteFile(filename, contents, 0600); err != nil {
					return err
				}
			}
			return nil
		},
	}

	flag := rootCmd.PersistentFlags()
	flag.BoolVar(&opts.AllowMissingProviders, "allow-missing-plugins", false,
		"allows code generation to continue if resource provider plugins are missing")
	flag.BoolVar(&opts.AllowMissingVariables, "allow-missing-variables", false,
		"allows code generation to continue if the config references missing variables")
	flag.BoolVar(&opts.AllowMissingComments, "allow-missing-comments", true,
		"allows code generation to continue if there are errors extracting comments")
	flag.BoolVar(&opts.AnnotateNodesWithLocations, "record-locations", false,
		"annotate the generated code with original source locations for each resource")
	flag.BoolVar(&opts.ConvertToComponentResource, "convert-to-component-resource", false,
		"converts the project to a Pulumi Component Resource if true, and to a standard program if false")
	flag.BoolVar(&tarout, "tar", false,
		"generate a TAR archive to stdout instead of writing to the filesystem")
	flag.StringVar(&resourceNameProperty, "filter-resource-names", "",
		"when set, the property with the given key will be removed from all resources")
	flag.BoolVar(&filterAutoNames, "filter-auto-names", false,
		"when set, properties that are auto-generated names will be removed from all resources")
	flag.StringVar(&opts.TargetLanguage, "target-language", "typescript",
		"sets the language to target")
	flag.StringVar(&opts.TargetSDKVersion, "target-sdk-version", "0.17.28",
		"sets the language SDK version to target")
	flag.StringVar(&opts.TerraformVersion, "terraform-version", "11",
		"sets the Terraform version targeted by the source config")
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version number of tf2pulumi",
		Long:  `All software has versions. This is tf2pulumi's.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(-1)
	}
}

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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pulumi/tf2pulumi/convert"
	"github.com/pulumi/tf2pulumi/version"
)

func main() {
	var opts convert.Options
	resourceNameProperty := ""

	rootCmd := &cobra.Command{
		Use:   "tf2pulumi",
		Short: "tf2pulumi converts Terraform configuration to a Pulumi TypeScript program",
		Long: `A converter that takes Terraform configuration as input and produces a
Pulumi TypeScript program that describes the same resource graph.`,

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(cmd *cobra.Command, args []string) error {
			if resourceNameProperty != "" {
				opts.FilterResourceNames, opts.ResourceNameProperty = true, resourceNameProperty
			}
			return convert.Convert(opts)
		},
	}

	flag := rootCmd.PersistentFlags()
	flag.BoolVar(&opts.AllowMissingProviders, "allow-missing-plugins", false,
		"allows code generation to continue if resource provider plugins are missing")
	flag.BoolVar(&opts.AllowMissingVariables, "allow-missing-variables", false,
		"allows code generation to continue if the config references missing variables")
	flag.BoolVar(&opts.AllowMissingComments, "allow-missing-comments", true,
		"allows code generation to continue if there are errors extracting comments")
	flag.StringVar(&resourceNameProperty, "filter-resource-names", "",
		"when set, the property with the given key will be removed from all resources")
	flag.StringVar(&opts.TargetLanguage, "target-language", "typescript",
		"sets the target language")
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

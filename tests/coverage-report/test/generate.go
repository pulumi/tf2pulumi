// Copyright 2021, Pulumi Corporation. All rights reserved

// +build ignore

package main

import (
	"flag"
	"fmt"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"github.com/russross/blackfriday/v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

var testOutputDir = flag.String("testOutputDir", "test-results",
	`location to write raw test output to. Defaults to ./test-results. Creates the folder if it 
	does not exist.`)
var testInputDir = flag.String("testInputDir", "../testdata/example-snippets",
	`location to write example snippets to be used for input. Defaults to 
	../testdata/example-snippets. Creates the folder if it does not exist.`)
var retainConverted = flag.Bool("retainConverted", false,
	"When set to true retains the converted files in 'testOutputDir'")

// Generates the example snippets which are then provided as test input for the tf2pulumi coverage
// report.
func main() {
	contract.AssertNoError(os.MkdirAll(*testInputDir, 0700))

	// Calls to genProvider to create snippets for each provider.
	// The test input can be added to by simply adding more genProvider calls with proper parameters.
	genProvider("aws", "../testdata/terraform-provider-aws/website/docs/r/*")
	genProvider("azurerm", "../testdata/terraform-provider-azurerm/website/docs/r/*")
	genProvider("google", "../testdata/terraform-provider-google/website/docs/r/*")
}

// This sets up generation of the snippets for a particular provider.
// providerName - the name of the provider
// providerDocsPath - the path to the docs to generate snippets from
func genProvider(providerName string, providerDocsPath string) {
	providerPath := filepath.Join(*testInputDir, providerName)
	contract.AssertNoError(os.MkdirAll(providerPath, 0700))
	genProviderSnippets(providerPath, providerDocsPath)
}

// This generates the actual snippets for the provided path and into the provided directory
// providerSnippetsDir - the directory to the parsed snippets
// providerDocsPath - the path to the docs to generate snippets from
func genProviderSnippets(providerSnippetsDir string, providerDocsPath string) {
	matches, err := filepath.Glob(providerDocsPath)
	contract.AssertNoError(err)

	for _, match := range matches {
		counter := 0

		// Make directory to store all snippets for the current resource
		currResource := strings.ReplaceAll(filepath.Base(match), ".html.markdown", "")
		currResourceSnippetsDir := filepath.Join(providerSnippetsDir, currResource)
		contract.AssertNoError(os.MkdirAll(currResourceSnippetsDir, 0700))
		mdDocContent, err := ioutil.ReadFile(match)
		contract.AssertNoError(err)

		md := blackfriday.New(blackfriday.WithExtensions(blackfriday.FencedCode))
		rootNode := md.Parse(mdDocContent)
		rootNode.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
			// Only read each node on entry not exit as well and if the node is a code block
			// of either terraform or hcl
			if entering &&
				node.Type == blackfriday.CodeBlock &&
				string(node.CodeBlockData.Info) == "terraform" ||
				string(node.CodeBlockData.Info) == "hcl" {

				// Keeps track of what number example we are looking at for a particular resource
				counter++
				// Make directory for snippet
				var snippetDir string
				// If there is a header for the snippet use that as it's title
				if node.Prev != nil && node.Prev.Type == blackfriday.Heading && node.Prev.FirstChild != nil {
					snippetName := string(node.Prev.FirstChild.Literal)
					snippetDir = filepath.Join(currResourceSnippetsDir, snippetName)
					snippetDir = strings.ReplaceAll(snippetDir, "/ ", "-")
					snippetDir = strings.ReplaceAll(snippetDir, " ", "-")
					snippetDir = strings.ToLower(snippetDir)
				} else { // Else use a numbered title (based on counter)
					snippetName := fmt.Sprintf("%s%d", "example-usage", counter)
					snippetDir = filepath.Join(currResourceSnippetsDir, snippetName)
				}
				contract.AssertNoError(os.MkdirAll(snippetDir, 0700))

				// Write snippet to a `main.tf` file in the newly created snippet directory
				snippetFile := filepath.Join(snippetDir, "main.tf")
				contract.AssertNoError(ioutil.WriteFile(snippetFile, node.Literal, 0600))
			}
			return blackfriday.GoToNext
		})
	}
}

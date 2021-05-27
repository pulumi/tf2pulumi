// Copyright 2021, Pulumi Corporation. All rights reserved

// coverage

package test

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tf2pulumi/convert"
	"github.com/russross/blackfriday/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	// "q"
)

var testOutputDir = flag.String("testOutputDir", "test-results", "location to write raw test output to. Defaults to ./test-results. Creates the folder if it does not exist.")
var testInputDir = flag.String("testInputDir", "../testdata/example-snippets", "location to write example snippets to be used for input. Defaults to ../testdata/example-snippets. Creates teh folder if it does not exist.")
var retainConverted = flag.Bool("retainConverted", false, "When set to true retains the converted files in 'testOutputDir'")

// These templates currently cause panics.
var excluded = map[string]bool{
	// "../testdata/terraform-guides/infrastructure-as-code/k8s-cluster-openshift-aws": true,
	// "../testdata/terraform-guides/infrastructure-as-code/README.md": true,
	// "../testdata/terraform-provider-aws/examples/README.md": true,
	"../testdata/example-snippets/aws/autoscaling_group/example-usage": true,
}

func TestTemplateCoverage(t *testing.T) {
	matches, err := filepath.Glob("../testdata/example-snippets/aws/*/*")
	// matches, err := filepath.Glob("../testdata/terraform-provider-aws/examples/*")
	// matches, err := filepath.Glob("../testdata/terraform-guides/infrastructure-as-code/*")
	// matches, err := filepath.Glob("./temp-tf2pulumi")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(*testOutputDir, 0700))

	t.Logf("Test outputs will be logged to: %s", *testOutputDir)

	var diagList []Diag
	for _, match := range matches {
		t.Run(match, func(t *testing.T) {
			if excluded[match] {
				t.Skip()
			}
			// t.Logf("Current match: %s", match)
			snippet := filepath.Base(match)
			snippetResource := filepath.Base(filepath.Dir(match))
			template := snippetResource + "-" + snippet // Edit as needed to match format of data

			// defer func() {
			// 	v := recover()
			// 	if v != nil {
			// 		t.Logf("Panic in template: %s", template)
			// 		diagList = append(diagList, Diag{
			// 			CoverageDiag: CoverageDiag{
			// 				Summary: "Panic",
			// 				Severity: "Fatal",
			// 			},
			// 			Snippet: snippet,
			// 			Resource: snippetResource,
			// 		})
			// 	}
			// }()	
			
			var opts convert.Options
			opts.TargetLanguage = "typescript"
			opts.Root = afero.NewBasePathFs(afero.NewOsFs(), match);
			body, diag, err := convert.Convert(opts)
			// fmt.Println(body)
			// t.Logf("Current template: %s", template)
			if err != nil {
				diagList = append(diagList, Diag{
					CoverageDiag: CoverageDiag{
						Summary: err.Error(),
						Severity: "Fatal",
					},
					Snippet: snippet,
					Resource: snippetResource,
				})
			} else { // err == nil
				if len(diag.All) == 0 {
					// Success is represented by no diagnostic information apart from template name.
					diagList = append(diagList, Diag{Snippet: snippet, Resource: snippetResource})
				}
				for _, d := range diag.All {
					mappedDiag := mapDiag(t, d, match)
					diagList = append(diagList, Diag{CoverageDiag: mappedDiag, Snippet: snippet, Resource: snippetResource, RawDiag: d})
				}
				if *retainConverted {
					require.NoError(t, os.MkdirAll(filepath.Join(*testOutputDir, template), 0700))
					for fileName, fileContent := range body {
						require.NoError(t, ioutil.WriteFile(filepath.Join(*testOutputDir, template, fileName), fileContent, 0600))
					}
				}
			}
		})
	}
	summarize(t, diagList)
}

// Maps tf2pulumi's diagnostics to our CoverageDiag, removing information we do not need for querying.
// The full diagonstic is still stored in case that information is considered valuable.
func mapDiag(t *testing.T, rawDiag *hcl.Diagnostic, matchDir string) CoverageDiag {
	var newDiag CoverageDiag
	newDiag.Summary = rawDiag.Summary
	if (rawDiag.Severity == hcl.DiagError) {
		newDiag.Severity = "High"
	} else if (rawDiag.Severity == hcl.DiagWarning) { // should be only other case
		newDiag.Severity = "Med"
	} else {
		t.Errorf("Got a rawDiagnostic Severity of: %d", rawDiag.Severity)
	}
	newDiag.Subject = rawDiag.Subject

	fileToRead := filepath.Join(matchDir, rawDiag.Subject.Filename)
	// t.Logf("new file to read: %s", fileToRead)
	fileContent, err := ioutil.ReadFile(fileToRead)
	if (err != nil) {
		newDiag.FileContent = "Could not load file content"
	} else { // err == nil
		newDiag.FileContent = string(fileContent)
	}
	// require.NoError(t, err)
	return newDiag
}

// The struct used to define the overall diagnostic we keep track of
type Diag struct {
	CoverageDiag
	Snippet string `json:"snippet"`
	Resource string `json:"resource"`
	RawDiag *hcl.Diagnostic `json:"rawDiagnostic"`
}

// The struct used to define the diagnositics of a single tf2pulumi call
type CoverageDiag struct {
	Summary string `json:"summary"`
	Severity string `json:"severity"`
	Subject *hcl.Range `json:"subject"`
	FileContent string `json:"fileContent"`
}

type Result struct {
	Number int
	Pct    float32
}

type OverallResult struct {
	NoErrors, LowSevErrors, HighSevErrors, Fatal Result
	Total                                        int
}

func summarize(t *testing.T, diagList []Diag) {
	jsonOutputLocation := filepath.Join(*testOutputDir, "summary.json")
	marshalled, err := json.MarshalIndent(diagList, "", "\t")
	require.NoError(t, err)
	require.NoError(t, ioutil.WriteFile(jsonOutputLocation, marshalled, 0600))

	db, err := sql.Open("sqlite", filepath.Join(*testOutputDir, "summary.db"))
	require.NoError(t, err)
	_, err = db.Exec(`
				DROP TABLE IF EXISTS errors;
				CREATE TABLE errors(
					resource TEXT NOT NULL,
					snippet TEXT NOT NULL,
					severity TEXT,
					subject TEXT,
					summary TEXT,
					file_content TEXT,
					raw_diagnostic TEXT,
					PRIMARY KEY (resource, snippet, severity, subject, summary, file_content, raw_diagnostic)
				);`)
	require.NoError(t, err)

	nullable := func(val string) sql.NullString {
		if val == "" {
			return sql.NullString{}
		}
		return sql.NullString{
			String: val,
			Valid:  true,
		}
	}
	for _, d := range diagList {
		marshalledSubject, err := json.MarshalIndent(d.Subject, "", "\t")
		require.NoError(t, err)
		marshalledRawDiag, err := json.MarshalIndent(d.RawDiag, "", "\t")
		require.NoError(t, err)		

		_, err = db.Exec(`INSERT INTO errors(
                   resource,
				   snippet,
                   severity,
                   subject,
                   summary,
                   file_content,
				   raw_diagnostic
        	) values(?, ?, ?, ?, ?, ?, ?)`,
			d.Resource,
			d.Snippet,
			nullable(d.Severity),
			nullable(string(marshalledSubject)),
			nullable(d.Summary),
			nullable(d.FileContent),
			nullable(string(marshalledRawDiag)))

			require.NoError(t, err)
	}

	var numSnippets, fatalErrors, highSevErrors, medSevErrors, success int

	countAllQuery := `SELECT COUNT(*) 
					FROM (
						SELECT DISTINCT resource, snippet 
						FROM errors
						)`
	row := db.QueryRow(countAllQuery)
	require.NoError(t, row.Scan(&numSnippets))

	countGenQuery := `SELECT COUNT(*) 
					FROM (
						SELECT DISTINCT resource, snippet 
						FROM errors
						WHERE severity=?
						)`

	row = db.QueryRow(countGenQuery, "Fatal")
	require.NoError(t, row.Scan(&fatalErrors))

	row = db.QueryRow(countGenQuery, "High")
	require.NoError(t, row.Scan(&highSevErrors))

	row = db.QueryRow(countGenQuery, "Med")
	require.NoError(t, row.Scan(&medSevErrors))

	countSuccessQuery := `SELECT COUNT(*) 
							FROM (
								SELECT DISTINCT resource, snippet 
								FROM errors
								WHERE severity is NULL
								)`
	row = db.QueryRow(countSuccessQuery)
	require.NoError(t, row.Scan(&success))

	// Stores the overall results in a JSON object to compare in future tests.
	data := OverallResult{
		NoErrors:      Result{success, float32(success) / float32(numSnippets) * 100.0},
		LowSevErrors:  Result{medSevErrors, float32(medSevErrors) / float32(numSnippets) * 100.0},
		HighSevErrors: Result{highSevErrors, float32(highSevErrors) / float32(numSnippets) * 100.0},
		Fatal:         Result{fatalErrors, float32(fatalErrors) / float32(numSnippets) * 100.0},
		Total:         numSnippets,
	}

	file, err := json.MarshalIndent(data, "", "\t")
	require.NoError(t, err)

	// Stores JSON result in "results.json" file in current directory.
	require.NoError(t, ioutil.WriteFile("results.json", file, 0600))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Overall Summary of Conversions")
	table.SetHeader([]string{"Result", "Number", "Perc."})
	table.Append([]string{"No Errors", fmt.Sprintf("%d", success), fmt.Sprintf("%.2f%%", float32(success)/float32(numSnippets)*100.0)})
	table.Append([]string{"Low Sev. Errors", fmt.Sprintf("%d", medSevErrors), fmt.Sprintf("%.2f%%", float32(medSevErrors)/float32(numSnippets)*100.0)})
	table.Append([]string{"High Sev. Errors", fmt.Sprintf("%d", highSevErrors), fmt.Sprintf("%.2f%%", float32(highSevErrors)/float32(numSnippets)*100.0)})
	table.Append([]string{"Fatal", fmt.Sprintf("%d", fatalErrors), fmt.Sprintf("%.2f%%", float32(fatalErrors)/float32(numSnippets)*100.0)})
	table.Append([]string{"Total", fmt.Sprintf("%d", numSnippets), ""})
	table.Render()
	fmt.Print("\n\n")

	table = tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Top Reasons For Fatal Errors")
	table.SetRowLine(true)
	table.SetHeader([]string{"# of Times", "Reason"})
	var desc string
	var cnt int
	rows, err := db.Query(`SELECT summary, COUNT(*) AS cnt FROM errors WHERE severity='Fatal' GROUP BY summary ORDER BY cnt DESC LIMIT 5`)
	require.NoError(t, err)
	for rows.Next() {
		rows.Scan(&desc, &cnt)
		table.Append([]string{fmt.Sprintf("%d", cnt), desc})
	}
	table.Render()
	fmt.Print("\n\n")

	table = tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Top Reasons For High Sev Errors")
	table.SetRowLine(true)
	table.SetHeader([]string{"# of Times", "Reason"})
	rows, err = db.Query(`SELECT summary, COUNT(*) AS cnt FROM errors WHERE severity='High' GROUP BY summary ORDER BY cnt DESC LIMIT 5`)
	require.NoError(t, err)
	for rows.Next() {
		rows.Scan(&desc, &cnt)
		table.Append([]string{fmt.Sprintf("%d", cnt), desc})
	}
	table.Render()
}

func TestGenInput(t *testing.T) {
	// t.Logf("Test outputs will be logged to: %s", *testOutputDir)

	require.NoError(t, os.MkdirAll(*testInputDir, 0700))

	t.Run("aws", func(t *testing.T) {
		providerPath := filepath.Join(*testInputDir, "aws")
		require.NoError(t, os.MkdirAll(providerPath, 0700))
		providerDocsPath := "../testdata/terraform-provider-aws/website/docs/r/*"
		genProviderSnippets(t, providerPath, providerDocsPath)
	})
}

func genProviderSnippets(t *testing.T, providerSnippetsDir string, providerDocsPath string) {
	matches, err := filepath.Glob(providerDocsPath)
	require.NoError(t, err)
	// q.Q(matches)

	for _, match := range matches {
		counter := 0
		// t.Logf("Current match: %s", match)
		
		// Make directory to store all snippets for the current resource
		currResource := strings.ReplaceAll(filepath.Base(match), ".html.markdown", "")
		currResourceSnippetsDir := filepath.Join(providerSnippetsDir, currResource)
		require.NoError(t, os.MkdirAll(currResourceSnippetsDir, 0700))
		mdDocContent, err := ioutil.ReadFile(match)
		require.NoError(t, err)

		md := blackfriday.New(blackfriday.WithExtensions(blackfriday.FencedCode))
		rootNode := md.Parse(mdDocContent)
		rootNode.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
			if (entering) {
				if node.Type == blackfriday.CodeBlock && (string(node.CodeBlockData.Info) == "terraform" || string(node.CodeBlockData.Info) == "hcl") {
					counter += 1
					// Make directory for snippet
					var snippetDir string
					// If there is a header for the snippet use that as it's title
					if node.Prev != nil && node.Prev.Type == blackfriday.Heading && node.Prev.FirstChild != nil {
						snippetName := string(node.Prev.FirstChild.Literal)
						snippetDir = filepath.Join(currResourceSnippetsDir, snippetName)
						snippetDir = strings.ReplaceAll(snippetDir, "/ ", "-")
						snippetDir = strings.ReplaceAll(snippetDir, " ", "-")
						snippetDir = strings.ToLower(snippetDir)
					} else { // Else use a numbered title
						snippetName := fmt.Sprintf("%s%d", "example-usage", counter)
						snippetDir = filepath.Join(currResourceSnippetsDir, snippetName)
					}
					os.MkdirAll(snippetDir, 0700)
						
					// Write snippet to a `main.tf` file in the newly created snippet directory
					snippetFile := filepath.Join(snippetDir, "main.tf")
					require.NoError(t, ioutil.WriteFile(snippetFile, node.Literal, 0600))
					
					// q.Q(string(node.Prev.FirstChild.Literal))
					// printNodeInfo(node)
					// q.Q(fmt.Sprint(node.CodeBlockData))
					// q.Q(node.CodeBlockData)
					// q.Q(string(node.CodeBlockData.Info))
					// q.Q("---------")
				}
			}
			return blackfriday.GoToNext
		})
	}
}

// func printNodeInfo(node *blackfriday.Node) {
// 	q.Q(fmt.Sprint(node.Type))
// 	q.Q(string(node.Title))
// 	q.Q(string(node.Literal))
// 	// q.Q(string(node.FirstChild.Literal))
// 	q.Q(fmt.Sprint(node.Parent))
// }

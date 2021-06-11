// Copyright 2021, Pulumi Corporation. All rights reserved

// coverage

package test

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/olekukonko/tablewriter"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tf2pulumi/convert"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

var testOutputDir = flag.String("testOutputDir", "test-results",
	`location to write raw test output to. Defaults to ./test-results. Creates the folder if it 
	does not exist.`)
var retainConverted = flag.Bool("retainConverted", false,
	"When set to true retains the converted files in 'testOutputDir'")

// These templates currently cause panics.
// Uncomment them if you want to ignore these examples, otherwise they will be recorded as panics
// due to the defer function on line 73.
var excluded = map[string]bool{
	// "../testdata/example-snippets/aws/autoscaling_group/example-usage": true,
	// "../testdata/example-snippets/azurerm/spatial_anchors_account/example-usage": true,
	// "../testdata/example-snippets/azurerm/spring_cloud_app_cosmosdb_association/example-usage": true,
	// "../testdata/example-snippets/azurerm/monitor_scheduled_query_rules_log/example-usage": true,
	`../testdata/example-snippets/google/google_service_account_key/example-usage,-save-key-in-
	kubernetes-secret---deprecated`: true,
	// "../testdata/example-snippets/google/cloud_run_service/example-usage---cloud-run-anthos": true,
}

//go:generate go run generate.go
func TestTemplateCoverage(t *testing.T) {
	matches, err := filepath.Glob("../testdata/example-snippets/*/*/*")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(*testOutputDir, 0700))

	t.Logf("Test outputs will be logged to: %s", *testOutputDir)

	var diagList []Diag
	for _, match := range matches {
		t.Run(match, func(t *testing.T) {
			if excluded[match] {
				t.Skip()
			}
			snippet := filepath.Base(match)
			snippetResource := filepath.Base(filepath.Dir(match))
			snippetProvider := filepath.Base(filepath.Dir(filepath.Dir(match)))
			template := snippetResource + "-" + snippet // Edit as needed to match format of data

			// Comment out if we don't want to record panics
			// If commented out requires manually adding panic causing files to `excluded`
			defer func() {
				v := recover()
				if v != nil {
					t.Logf("Panic in template: %s", template)
					diagList = append(diagList, Diag{
						CoverageDiag: CoverageDiag{
							Summary:  "Panic",
							Severity: "Fatal",
						},
						Snippet:  snippet,
						Resource: snippetResource,
						Provider: snippetProvider,
					})
				}
			}()

			var opts convert.Options
			opts.TargetLanguage = "typescript"
			opts.Root = afero.NewBasePathFs(afero.NewOsFs(), match)
			body, diag, err := convert.Convert(opts)

			if err != nil {
				diagList = append(diagList, Diag{
					CoverageDiag: CoverageDiag{
						Summary:  err.Error(),
						Severity: "Fatal",
					},
					Snippet:  snippet,
					Resource: snippetResource,
					Provider: snippetProvider,
				})
			} else { // err == nil
				if len(diag.All) == 0 {
					// Success is represented by no diagnostic information apart from snippet info.
					diagList = append(diagList, Diag{
						Snippet:  snippet,
						Resource: snippetResource,
						Provider: snippetProvider,
					})
				}
				for _, d := range diag.All {
					mappedDiag := mapDiag(t, d, match)
					diagList = append(diagList, Diag{
						CoverageDiag: mappedDiag,
						Snippet:      snippet,
						Resource:     snippetResource,
						Provider:     snippetProvider,
						RawDiag:      d,
					})
				}
				if *retainConverted {
					require.NoError(t, os.MkdirAll(filepath.Join(*testOutputDir, template), 0700))
					for fileName, fileContent := range body {
						path := filepath.Join(*testOutputDir, template, fileName)
						require.NoError(t, ioutil.WriteFile(path, fileContent, 0600))
					}
				}
			}
		})
	}
	summarize(t, diagList)
}

// Maps tf2pulumi's diagnostics to our CoverageDiag, removing information we do not need for querying.
// The full diagnostic is still stored in case that information is considered valuable.
func mapDiag(t *testing.T, rawDiag *hcl.Diagnostic, matchDir string) CoverageDiag {
	var newDiag CoverageDiag
	newDiag.Summary = rawDiag.Summary
	if rawDiag.Severity == hcl.DiagError {
		newDiag.Severity = "High"
	} else if rawDiag.Severity == hcl.DiagWarning { // should be only other case
		newDiag.Severity = "Med"
	} else {
		newDiag.Severity = "High" // This way we still get logs, but it wasn't Fatal since there was no panic
	}
	newDiag.Subject = rawDiag.Subject

	fileToRead := filepath.Join(matchDir, rawDiag.Subject.Filename)
	fileContent, err := ioutil.ReadFile(fileToRead)
	if err != nil {
		newDiag.FileContent = "Could not load file content"
	} else { // err == nil
		newDiag.FileContent = string(fileContent)
	}
	return newDiag
}

// The struct used to define the overall diagnostic we keep track of
type Diag struct {
	CoverageDiag
	Snippet  string          `json:"snippet"`
	Resource string          `json:"resource"`
	Provider string          `json:"provider"`
	RawDiag  *hcl.Diagnostic `json:"rawDiagnostic"`
}

// The struct used to define the diagnositics of a single tf2pulumi call
type CoverageDiag struct {
	Summary     string     `json:"summary"`
	Severity    string     `json:"severity"`
	Subject     *hcl.Range `json:"subject"`
	FileContent string     `json:"fileContent"`
}

type Result struct {
	Number int
	Pct    float32
}

type OverallResult struct {
	NoErrors, LowSevErrors, HighSevErrors, Fatal Result
	Total                                        int
}

// Summarizes the results in a JSON file and sqlite database. Also generates `results.json` which
// contains overall results.
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
					provider TEXT NOT NULL,
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
					provider,
                   	resource,
					snippet,
					severity,
					subject,
					summary,
					file_content,
					raw_diagnostic
        	) values(?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Provider,
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
	successPct := float32(success) / float32(numSnippets) * 100.0
	medSevPct := float32(medSevErrors) / float32(numSnippets) * 100.0
	highSevPct := float32(highSevErrors) / float32(numSnippets) * 100.0
	fatalPct := float32(fatalErrors) / float32(numSnippets) * 100.0
	data := OverallResult{
		NoErrors:      Result{success, successPct},
		LowSevErrors:  Result{medSevErrors, medSevPct},
		HighSevErrors: Result{highSevErrors, highSevPct},
		Fatal:         Result{fatalErrors, fatalPct},
		Total:         numSnippets,
	}

	file, err := json.MarshalIndent(data, "", "\t")
	require.NoError(t, err)

	// Stores JSON result in "results.json" file in current directory.
	require.NoError(t, ioutil.WriteFile("results.json", file, 0600))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Overall Summary of Conversions")
	table.SetHeader([]string{"Result", "Number", "Perc."})
	table.Append([]string{"No Errors", fmt.Sprintf("%d", success), fmt.Sprintf("%.2f%%", successPct)})
	table.Append([]string{"Low Sev. Errors", fmt.Sprintf("%d", medSevErrors), fmt.Sprintf("%.2f%%", medSevPct)})
	table.Append([]string{"High Sev. Errors", fmt.Sprintf("%d", highSevErrors), fmt.Sprintf("%.2f%%", highSevPct)})
	table.Append([]string{"Fatal", fmt.Sprintf("%d", fatalErrors), fmt.Sprintf("%.2f%%", fatalPct)})
	table.Append([]string{"Total", fmt.Sprintf("%d", numSnippets), ""})
	table.Render()
	fmt.Print("\n\n")

	table = tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Top Reasons For Fatal Errors")
	table.SetRowLine(true)
	table.SetHeader([]string{"# of Times", "Reason"})
	var desc string
	var cnt int
	rows, err := db.Query(`SELECT summary, COUNT(*) AS cnt 
						   FROM errors 
						   WHERE severity='Fatal' 
						   GROUP BY summary 
						   ORDER BY cnt DESC LIMIT 5`)
	require.NoError(t, err)
	for rows.Next() {
		require.NoError(t, rows.Scan(&desc, &cnt))
		table.Append([]string{fmt.Sprintf("%d", cnt), desc})
	}
	table.Render()
	fmt.Print("\n\n")

	table = tablewriter.NewWriter(os.Stdout)
	table.SetCaption(true, "Top Reasons For High Sev Errors")
	table.SetRowLine(true)
	table.SetHeader([]string{"# of Times", "Reason"})
	rows, err = db.Query(`SELECT summary, COUNT(*) AS cnt 
						  FROM errors 
						  WHERE severity='High' 
						  GROUP BY summary 
						  ORDER BY cnt DESC LIMIT 5`)
	require.NoError(t, err)
	for rows.Next() {
		require.NoError(t, rows.Scan(&desc, &cnt))
		table.Append([]string{fmt.Sprintf("%d", cnt), desc})
	}
	table.Render()
}

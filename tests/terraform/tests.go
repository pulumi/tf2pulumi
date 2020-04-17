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

// Package terraform provides a test harness for tf2pulumi. It is a thin wrapper on top of the Pulumi CLI's integration
// test framework while supplying a "compile step" for Terraform code that we'll then convert to Pulumi code.
package terraform

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/pulumi/pkg/v2/testing/integration"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/fsutil"
)

// Test represents a single test case. It consists of Terraform input and optionally a "baseline" file that will be
// diffed against the output of tf2pulumi. Each test case is run on every target unless the target specifically opts
// out.
type Test struct {
	ProjectName string
	Options     ConvertOptions                  // Base options for tf2pulumi, inherited by all targets.
	RunOptions  *integration.ProgramTestOptions // Options for running the generated Pulumi code
	Python      *ConvertOptions                 // Python-specific options overriding the base options
	TypeScript  *ConvertOptions                 // TypeScript-specific options overriding the base options
}

// ConvertOptions are options controlling the behavior of tf2pulumi. Its arguments are generally converted to
// command-line flags.
type ConvertOptions struct {
	Compile    *bool  // If true, run pulumi on the generated code. Defaults to true.
	FilterName string // If non-empty, filter out properties with the given name.
	Skip       string // If non-empty, skip the current test with the given message.
}

// With constructs a new ConvertOptions out of a base set of options and a set of options that will override fields that
// are not set in the base.
func (c ConvertOptions) With(other ConvertOptions) ConvertOptions {
	if other.Compile != nil {
		c.Compile = other.Compile
	}
	if other.FilterName != "" {
		c.FilterName = other.FilterName
	}
	if other.Skip != "" {
		c.Skip = other.Skip
	}
	return c
}

// Run executes this test, spawning subtests for each supported target.
func (test Test) Run(t *testing.T) {
	t.Helper()
	t.Parallel()
	t.Run("Python", func(t *testing.T) {
		runOpts := integration.ProgramTestOptions{}
		if test.RunOptions != nil {
			runOpts = *test.RunOptions
		}
		convertOpts := test.Options
		if test.Python != nil {
			convertOpts = convertOpts.With(*test.Python)
		}

		targetTest := targetTest{
			runOpts:     &runOpts,
			convertOpts: &convertOpts,
			projectName: test.ProjectName,
			language:    "python",
			runtime:     "python",
		}
		targetTest.Run(t)
	})
	t.Run("TypeScript", func(t *testing.T) {
		runOpts := integration.ProgramTestOptions{}
		if test.RunOptions != nil {
			runOpts = *test.RunOptions
		}
		convertOpts := test.Options
		if test.TypeScript != nil {
			convertOpts = convertOpts.With(*test.TypeScript)
		}

		targetTest := targetTest{
			runOpts:     &runOpts,
			convertOpts: &convertOpts,
			projectName: test.ProjectName,
			language:    "typescript",
			runtime:     "nodejs",
		}
		targetTest.Run(t)
	})
}

// targetTest is a test case running on a single target.
type targetTest struct {
	runOpts     *integration.ProgramTestOptions
	convertOpts *ConvertOptions
	projectName string
	language    string
	runtime     string
}

// Run runs the test case on the given target. It is responsible for driving tf2pulumi and the CLI integration test
// framework to compile and run a program.
func (test *targetTest) Run(t *testing.T) {
	if test.convertOpts.Skip != "" {
		t.Skip(test.convertOpts.Skip)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("expected a valid working directory: %v", err)
	}

	// Copy the stated directory to a temporary directory and stamp the temp dir over the original dir.
	targetDir, err := ioutil.TempDir("", "tf2pulumi-")
	if err != nil {
		t.Fatalf("failed to create intermediate directory: %v", err)
	}
	defer func() {
		contract.IgnoreError(os.RemoveAll(targetDir))
	}()
	if err = fsutil.CopyFile(targetDir, filepath.Join(cwd, test.runOpts.Dir), nil); err != nil {
		t.Fatalf("failed to create intermediate directory: %v", err)
	}
	test.runOpts.Dir = targetDir

	// Generate the Pulumi TypeScript program.
	test.generateCode(t)

	// If there is a baseline file, ensure that it matches.
	baselinePath := filepath.Join(targetDir, test.targetBaselineFile())
	baseline, err := ioutil.ReadFile(baselinePath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("failed to read baseline file %v: %v", baselinePath, err)
		}
	} else {
		programPath := filepath.Join(targetDir, test.targetFile())
		program, err := ioutil.ReadFile(programPath)
		if err != nil {
			t.Fatalf("failed to read program file %v: %v", programPath, err)
		}
		assert.Equalf(t, string(baseline), string(program),
			"baseline file %v does not match program file %v", baselinePath, programPath)
	}

	// Now, if desired, finally ensure that it actually compiles (and anything else the specific test requires).
	if test.convertOpts.Compile == nil || *test.convertOpts.Compile {
		// Emit the Pulumi project.
		project := fmt.Sprintf("name: %s\nruntime: %s\ndescription: %[1]s test\n", test.projectName, test.runtime)
		if err := ioutil.WriteFile(filepath.Join(targetDir, "Pulumi.yaml"), []byte(project), 0600); err != nil {
			t.Fatalf("failed to write Pulumi.yaml: %v", err)
		}

		integration.ProgramTest(t, test.runOpts)
	}
}

// targetFile returns the filename that tf2pulumi should write its output to.
func (test *targetTest) targetFile() string {
	switch test.language {
	case "python":
		return "__main__.py"
	case "typescript":
		return "index.ts"
	default:
		panic("unknown language")
	}
}

// targetBaselineFile returns the filename that, if the file exists, contains a baseline output that the test harness
// should diff against tf2pulumi's actual output.
func (test *targetTest) targetBaselineFile() string {
	switch test.language {
	case "python":
		return "__main__.base.py"
	case "typescript":
		return "index.base.ts"
	default:
		panic("unknown language")
	}
}

// generateCode drives terraform and tf2pulumi to convert the terraform code in the target directory to pulumi code.
func (test *targetTest) generateCode(t *testing.T) {
	stdout := test.runOpts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	fmt.Fprintf(stdout, "running `terraform init`...\n")

	// Run "terraform init".
	cmd := exec.Command("terraform", "init")
	cmd.Dir = test.runOpts.Dir
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		t.Fatalf("'terraform init' failed (%v): %v", cmdErr, string(out))
	}

	fmt.Fprintf(stdout, "running `tf2pulumi`...\n")

	// Generate an index.ts file using `tf2pulumi`.
	indexTS, err := os.Create(filepath.Join(test.runOpts.Dir, test.targetFile()))
	if err != nil {
		t.Fatalf("failed to create index.ts: %v", err)
	}
	defer contract.IgnoreClose(indexTS)

	var args []string
	if test.convertOpts.FilterName != "" {
		args = append(args, "--filter-resource-names="+test.convertOpts.FilterName)
	}
	args = append(args, "--target-language="+test.language)
	args = append(args, "--record-locations")

	var stderr bytes.Buffer
	cmd = exec.Command("tf2pulumi", args...)
	cmd.Dir = test.runOpts.Dir
	cmd.Stdout, cmd.Stderr = indexTS, &stderr
	if err = cmd.Run(); err != nil {
		t.Fatalf("failed to generate Pulumi program (%v):\n%v", err, stderr.String())
	}
}

//
// Everything below this point is sugar for manually constructing a `Test` structure.
//

// RunTest defines a new test case residing in the given directory. It takes zero or more options which can be used
// to further configure the test harness.
func RunTest(t *testing.T, dir string, opts ...TestOptionsFunc) {
	test := Test{}
	// Apply common defaults.
	test.ProjectName = filepath.Base(dir)
	test.Options.Compile = nil
	test.Options.FilterName = "name"
	test.RunOptions = &integration.ProgramTestOptions{
		Dir:                  dir,
		ExpectRefreshChanges: true,
	}
	for _, opt := range opts {
		opt(t, &test)
	}

	test.Run(t)
}

// TestOptionsFunc is a function that can be used as an option to `RunTest`.
type TestOptionsFunc func(*testing.T, *Test)

// Compile sets whether or not this test case should be executed by Pulumi after being processed by tf2pulumi. Defaults
// to true if not provided.
func Compile(value bool) TestOptionsFunc {
	return func(_ *testing.T, test *Test) { test.Options.Compile = &value }
}

// FilterName sets whether or not tf2pulumi should filter properties with the given name for this test case. Defaults
// to the empty string, which will cause no properties to be filtered.
func FilterName(value string) TestOptionsFunc {
	return func(_ *testing.T, test *Test) { test.Options.FilterName = value }
}

// Skip skips the test for the given reason.
func Skip(reason string) TestOptionsFunc {
	return func(_ *testing.T, test *Test) { test.Options.Skip = reason }
}

// AllowChanges allows changes on the empty preview and update for the given test.
func AllowChanges() TestOptionsFunc {
	return func(_ *testing.T, test *Test) {
		test.RunOptions.AllowEmptyPreviewChanges = true
		test.RunOptions.AllowEmptyUpdateChanges = true
	}
}

// Python sets up a new context that also accepts any number of test options, but applies those test options only when
// running with the Python target. Options that affect the Pulumi CLI integration test framework are ignored.
func Python(opts ...TestOptionsFunc) TestOptionsFunc {
	return func(t *testing.T, test *Test) {
		child := Test{}
		child.RunOptions = &integration.ProgramTestOptions{}
		for _, opt := range opts {
			opt(t, &child)
		}
		test.Python = &child.Options
	}
}

// SkipPython skips the Python target for the given test.
func SkipPython() TestOptionsFunc {
	return Python(Skip("Python not yet implemented"))
}

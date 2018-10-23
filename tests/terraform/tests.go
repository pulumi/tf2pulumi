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

package terraform

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi/pkg/testing/integration"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/util/fsutil"
)

func generateCode(t *testing.T, program *integration.ProgramTestOptions) {
	stdout := program.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	fmt.Fprintf(stdout, "running `terraform init`...\n")

	// Run "terraform init".
	cmd := exec.Command("terraform", "init")
	cmd.Dir = program.Dir
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		t.Fatalf("'terraform init' failed (%v): %v", cmdErr, string(out))
	}

	fmt.Fprintf(stdout, "running `tf2pulumi`...\n")

	// Generate an index.ts file using `tf2pulumi`.
	indexTS, err := os.Create(filepath.Join(program.Dir, "index.ts"))
	if err != nil {
		t.Fatalf("failed to create index.ts: %v", err)
	}
	defer contract.IgnoreClose(indexTS)

	var stderr bytes.Buffer
	cmd = exec.Command("tf2pulumi")
	cmd.Dir = program.Dir
	cmd.Stdout, cmd.Stderr = indexTS, &stderr
	if err = cmd.Run(); err != nil {
		t.Fatalf("failed to generate Pulumi program (%v):\n%v", err, stderr.String())
	}
}

func IntegrationTest(t *testing.T, program *integration.ProgramTestOptions) {
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
	if err = fsutil.CopyFile(targetDir, filepath.Join(cwd, program.Dir), nil); err != nil {
		t.Fatalf("failed to create intermediate directory: %v", err)
	}
	program.Dir = targetDir

	generateCode(t, program)

	integration.ProgramTest(t, program)
}

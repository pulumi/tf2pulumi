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
	"os"
	"testing"

	"github.com/pulumi/tf2pulumi/tests/terraform"
)

// RequireGCPConfig reads configuration for `gcp:project`, `gcp:region`, and `gcp:zone` from the `GOOGLE_PROJECT`,
// `GOOGLE_REGION`, and `GOOGLE_ZONE` environment variables, respectively. If any of these environment variables are
// not set, the test is skipped.
func RequireGCPConfig() terraform.TestOptionsFunc {
	return func(t *testing.T, test *terraform.Test) {
		// Set the configurations.
		project := os.Getenv("GOOGLE_PROJECT")
		if project == "" {
			t.Skipf("Skipping test due to missing GOOGLE_PROJECT variable")
		}
		region := os.Getenv("GOOGLE_REGION")
		if region == "" {
			t.Skipf("Skipping test due to missing GOOGLE_REGION variable")
		}
		zone := os.Getenv("GOOGLE_ZONE")
		if zone == "" {
			t.Skipf("Skipping test due to missing GOOGLE_ZONE variable")
		}
		if test.RunOptions.Config == nil {
			test.RunOptions.Config = make(map[string]string)
		}
		test.RunOptions.Config["gcp:project"] = project
		test.RunOptions.Config["gcp:region"] = region
		test.RunOptions.Config["gcp:zone"] = zone
	}
}

func RunGCPTest(t *testing.T, dir string, opts ...terraform.TestOptionsFunc) {
	opts = append(opts, RequireGCPConfig())
	terraform.RunTest(t, dir, opts...)
}

func TestRecordSet(t *testing.T) {
	RunGCPTest(t, "record_set", terraform.Compile(false))
}

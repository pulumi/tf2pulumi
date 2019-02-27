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

// RequireAWSRegion reads an AWS region from the `AWS_REGION` environment variable and sets the value of the
// `aws:region` Pulumi config key to the contents of the environment variable. If the environment variable is not set,
// the test is skipped.
func RequireAWSRegion() terraform.TestOptionsFunc {
	return func(t *testing.T, test *terraform.Test) {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			t.Skipf("Skipping test due to missing AWS_REGION environment variable")
		}
		if test.RunOptions.Config == nil {
			test.RunOptions.Config = make(map[string]string)
		}
		test.RunOptions.Config["aws:region"] = region
	}
}

func TestASG(t *testing.T) {
	terraform.RunTest(t, "asg",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestCognitoUserPool(t *testing.T) {
	terraform.RunTest(t, "cognito-user-pool",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestCount(t *testing.T) {
	terraform.RunTest(t, "count",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestECSALB(t *testing.T) {
	t.Skipf("Skipping test due to NYI: call to cidersubnet")
	terraform.RunTest(t, "ecs-alb",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestEIP(t *testing.T) {
	terraform.RunTest(t, "eip",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestELB(t *testing.T) {
	terraform.RunTest(t, "elb",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestELB2(t *testing.T) {
	terraform.RunTest(t, "elb2",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestLBListener(t *testing.T) {
	terraform.RunTest(t, "lb-listener",
		terraform.SkipPython(),
		RequireAWSRegion(),
		// Note we don't compile this one, since it contains semantic errors.
		terraform.Compile(false),
	)
}

func TestLambda(t *testing.T) {
	terraform.RunTest(t, "lambda",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

func TestNetworking(t *testing.T) {
	t.Skipf("Skipping test due to NYI: provider instances")
	terraform.RunTest(t, "networking",
		terraform.SkipPython(),
		RequireAWSRegion(),
	)
}

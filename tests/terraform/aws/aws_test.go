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

func RunAWSTest(t *testing.T, dir string, opts ...terraform.TestOptionsFunc) {
	opts = append(opts, RequireAWSRegion())
	terraform.RunTest(t, dir, opts...)
}

func TestASG(t *testing.T) {
	RunAWSTest(t, "asg")
}

func TestCognitoUserPool(t *testing.T) {
	RunAWSTest(t, "cognito-user-pool", terraform.AllowChanges())
}

func TestCount(t *testing.T) {
	RunAWSTest(t, "count")
}

func TestECSALB(t *testing.T) {
	t.Skipf("Skipping test due to NYI: call to cidrsubnet")
	RunAWSTest(t, "ecs-alb")
}

func TestEIP(t *testing.T) {
	RunAWSTest(t, "eip")
}

func TestELB(t *testing.T) {
	RunAWSTest(t, "elb")
}

func TestELB2(t *testing.T) {
	RunAWSTest(t, "elb2")
}

func TestLBListener(t *testing.T) {
	RunAWSTest(t, "lb-listener",
		// Note we don't compile this one, since it contains semantic errors.
		terraform.Compile(false),
	)
}

func TestLambda(t *testing.T) {
	RunAWSTest(t, "lambda")
}

func TestNetworking(t *testing.T) {
	t.Skipf("Skipping test due to NYI: call to cidrsubnet")
	RunAWSTest(t, "networking")
}

func TestEC2(t *testing.T) {
	RunAWSTest(t, "ec2",
		terraform.Compile(false),
	)
}

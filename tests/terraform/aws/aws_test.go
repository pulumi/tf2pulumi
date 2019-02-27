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
	"testing"

	"github.com/pulumi/tf2pulumi/tests/terraform"
)

func TestASG(t *testing.T) {
	terraform.RunTest(t, "asg",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestCognitoUserPool(t *testing.T) {
	terraform.RunTest(t, "cognito-user-pool",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestCount(t *testing.T) {
	terraform.RunTest(t, "count",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestECSALB(t *testing.T) {
	terraform.RunTest(t, "ecs-alb",
		terraform.Skip("Skipping test due to NYI: call to cidersubnet"),
		terraform.RequireAWSRegion(),
	)
}

func TestEIP(t *testing.T) {
	terraform.RunTest(t, "eip",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestELB(t *testing.T) {
	terraform.RunTest(t, "elb",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestELB2(t *testing.T) {
	terraform.RunTest(t, "elb2",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestLBListener(t *testing.T) {
	terraform.RunTest(t, "lb-listener",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
		// Note we don't compile this one, since it contains semantic errors.
		terraform.Compile(false),
	)
}

func TestLambda(t *testing.T) {
	terraform.RunTest(t, "lambda",
		terraform.SkipPython(),
		terraform.RequireAWSRegion(),
	)
}

func TestNetworking(t *testing.T) {
	terraform.RunTest(t, "networking",
		terraform.Skip("Skipping test due to NYI: provider instances"),
		terraform.RequireAWSRegion(),
	)
}

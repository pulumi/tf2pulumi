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

	"github.com/pulumi/pulumi/pkg/testing/integration"

	"github.com/pulumi/tf2pulumi/tests/terraform"
)

func integrationTest(t *testing.T, program *integration.ProgramTestOptions) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		t.Skipf("Skipping test due to missing AWS_REGION environment variable")
	}
	if program.Config == nil {
		program.Config = make(map[string]string)
	}
	program.Config["aws:region"] = region
	program.ExpectRefreshChanges = true

	terraform.IntegrationTest(t, program)
}

func TestASG(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "asg"})
}

func TestCognitoUserPool(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "cognito-user-pool"})
}

func TestCount(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "count"})
}

func TestECSALB(t *testing.T) {
	t.Skipf("Skipping test due to NYI: call to cidrsubnet")
	integrationTest(t, &integration.ProgramTestOptions{Dir: "ecs-alb"})
}

func TestEIP(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "eip"})
}

func TestELB(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "elb"})
}

func TestLambda(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "lambda"})
}

func TestNetworking(t *testing.T) {
	t.Skipf("Skipping test due to NYI: provider instances")
	integrationTest(t, &integration.ProgramTestOptions{Dir: "networking"})
}

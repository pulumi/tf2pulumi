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

func integrationTest(t *testing.T, program *integration.ProgramTestOptions, compile bool) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		t.Skipf("Skipping test due to missing AWS_REGION environment variable")
	}
	if program.Config == nil {
		program.Config = make(map[string]string)
	}
	program.Config["aws:region"] = region
	program.ExpectRefreshChanges = true

	terraform.IntegrationTest(t, program, compile)
}

func TestASG(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "asg"}, "name", true)
}

func TestCognitoUserPool(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "cognito-user-pool"}, "name", true)
}

func TestCount(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "count"}, "name", true)
}

func TestECSALB(t *testing.T) {
	t.Skipf("Skipping test due to NYI: call to cidrsubnet")
	integrationTest(t, &integration.ProgramTestOptions{Dir: "ecs-alb"}, "name", true)
}

func TestEIP(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "eip"}, "name", true)
}

func TestELB(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "elb"}, "name", true)
}

func TestELB2(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "elb2"}, "name", true)
}

func TestLBListener(t *testing.T) {
	// Note we don't compile this one, since it contains semantic errors.
	integrationTest(t, &integration.ProgramTestOptions{Dir: "lb-listener"}, "name", false)
}

func TestLambda(t *testing.T) {
	integrationTest(t, &integration.ProgramTestOptions{Dir: "lambda"}, "name", true)
}

func TestNetworking(t *testing.T) {
	t.Skipf("Skipping test due to NYI: provider instances")
	integrationTest(t, &integration.ProgramTestOptions{Dir: "networking"}, "name", true)
}

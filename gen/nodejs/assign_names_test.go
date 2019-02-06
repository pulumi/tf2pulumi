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

package nodejs

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/tokens"
	"github.com/pulumi/tf2pulumi/il"
	"github.com/stretchr/testify/assert"
)

func outputs(names []string) map[string]*il.OutputNode {
	m := make(map[string]*il.OutputNode)
	for _, name := range names {
		m[name] = &il.OutputNode{Config: &config.Output{Name: name}}
	}
	return m
}

func locals(names []string) map[string]*il.LocalNode {
	m := make(map[string]*il.LocalNode)
	for _, name := range names {
		m[name] = &il.LocalNode{Config: &config.Local{Name: name}}
	}
	return m
}

func variables(names []string) map[string]*il.VariableNode {
	m := make(map[string]*il.VariableNode)
	for _, name := range names {
		m[name] = &il.VariableNode{Config: &config.Variable{Name: name}}
	}
	return m
}

func modules(names []string) map[string]*il.ModuleNode {
	m := make(map[string]*il.ModuleNode)
	for _, name := range names {
		m[name] = &il.ModuleNode{Config: &config.Module{Name: name}}
	}
	return m
}

func resources(toks []string) map[string]*il.ResourceNode {
	m := make(map[string]*il.ResourceNode)
	for _, tok := range toks {
		// Split the token into a type and a name
		components := strings.Split(tok, "::")
		typ, name := tokens.Type(components[0]), components[1]

		mode := config.ManagedResourceMode
		if strings.HasPrefix(string(typ.Name()), "get") {
			mode = config.DataResourceMode
		}

		tfType := string(typ.Package()) + "_" +
			tfbridge.PulumiToTerraformName(string(typ.Module().Name())+string(typ.Name()), nil)

		// Create a provider for this resource
		provider := &il.ProviderNode{
			PluginName: string(typ.Package()),
			Info: &tfbridge.ProviderInfo{
				Resources: map[string]*tfbridge.ResourceInfo{
					tfType: &tfbridge.ResourceInfo{Tok: typ},
				},
				DataSources: map[string]*tfbridge.DataSourceInfo{
					tfType: &tfbridge.DataSourceInfo{Tok: tokens.ModuleMember(typ)},
				},
			},
		}

		m[tok] = &il.ResourceNode{
			Config: &config.Resource{
				Name: name,
				Type: tfType,
				Mode: mode,
			},
			Provider: provider,
		}
	}
	return m
}

func TestAssignNames(t *testing.T) {
	// Create a graph with a mix of ambiguous and non-ambiguous names.
	g := &il.Graph{
		Outputs: outputs([]string{
			"vpc_id",
			"security_group_id",
			"name",
		}),
		Locals: locals([]string{
			"vpc_id",
			"vpc",
			"name",
			"ec2",
		}),
		Variables: variables([]string{
			"region",
			"name",
			"instance",
			"default_vpc",
		}),
		Modules: modules([]string{
			"module",
			"name",
			"ec2",
		}),
		Resources: resources([]string{
			"aws:ec2:Vpc::cluster_vpc",
			"aws:ec2:Vpc::default",
			"aws:ec2:Instance::default",
			"aws:ec2:Instance::i",
			"aws:lightsail:Instance::main",
			"gcp:compute:Instance::main",
			"aws:ec2:getInstances::main",
		}),
	}

	names := assignNames(g, true)

	assert.Equal(t, "vpcId", names[g.Outputs["vpc_id"]])
	assert.Equal(t, "securityGroupId", names[g.Outputs["security_group_id"]])
	assert.Equal(t, "name", names[g.Outputs["name"]])

	assert.Equal(t, "myVpcId", names[g.Locals["vpc_id"]])
	assert.Equal(t, "vpc", names[g.Locals["vpc"]])
	assert.Equal(t, "myName", names[g.Locals["name"]])
	assert.Equal(t, "ec2", names[g.Locals["ec2"]])

	assert.Equal(t, "region", names[g.Variables["region"]])
	assert.Equal(t, "nameInput", names[g.Variables["name"]])
	assert.Equal(t, "instance", names[g.Variables["instance"]])
	assert.Equal(t, "defaultVpc", names[g.Variables["default_vpc"]])

	assert.Equal(t, "module", names[g.Modules["module"]])
	assert.Equal(t, "nameInstance", names[g.Modules["name"]])
	assert.Equal(t, "ec2Instance", names[g.Modules["ec2"]])

	assert.Equal(t, "clusterVpc", names[g.Resources["aws:ec2:Vpc::cluster_vpc"]])
	assert.Equal(t, "defaultEc2Vpc", names[g.Resources["aws:ec2:Vpc::default"]])
	assert.Equal(t, "defaultInstance", names[g.Resources["aws:ec2:Instance::default"]])
	assert.Equal(t, "awsEc2Instance", names[g.Resources["aws:ec2:Instance::i"]])
	assert.Equal(t, "mainInstance", names[g.Resources["aws:lightsail:Instance::main"]])
	assert.Equal(t, "mainComputeInstance", names[g.Resources["gcp:compute:Instance::main"]])
	assert.Equal(t, "mainInstances", names[g.Resources["aws:ec2:getInstances::main"]])
}

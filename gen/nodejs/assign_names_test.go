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

	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
)

func outputs(names []string) map[string]*il.OutputNode {
	m := make(map[string]*il.OutputNode)
	for _, name := range names {
		m[name] = &il.OutputNode{Name: name}
	}
	return m
}

func locals(names []string) map[string]*il.LocalNode {
	m := make(map[string]*il.LocalNode)
	for _, name := range names {
		m[name] = &il.LocalNode{Name: name}
	}
	return m
}

func variables(names []string) map[string]*il.VariableNode {
	m := make(map[string]*il.VariableNode)
	for _, name := range names {
		m[name] = &il.VariableNode{Name: name}
	}
	return m
}

func modules(names []string) map[string]*il.ModuleNode {
	m := make(map[string]*il.ModuleNode)
	for _, name := range names {
		m[name] = &il.ModuleNode{Name: name}
	}
	return m
}

func resources(toks []string) map[string]*il.ResourceNode {
	m := make(map[string]*il.ResourceNode)
	for _, tok := range toks {
		// Split the token into a type and a name
		components := strings.Split(tok, "::")
		typ, name := tokens.Type(components[0]), components[1]

		isDataSource := false
		if strings.HasPrefix(string(typ.Name()), "get") {
			isDataSource = true
		}

		tfType := string(typ.Package()) + "_" +
			tfbridge.PulumiToTerraformName(string(typ.Module().Name())+string(typ.Name()), nil, nil)

		// Create a provider for this resource
		provider := &il.ProviderNode{
			PluginName: string(typ.Package()),
			Info: &tfbridge.ProviderInfo{
				Resources: map[string]*tfbridge.ResourceInfo{
					tfType: {Tok: typ},
				},
				DataSources: map[string]*tfbridge.DataSourceInfo{
					tfType: {Tok: tokens.ModuleMember(typ)},
				},
			},
		}

		m[tok] = &il.ResourceNode{
			Name:         name,
			Type:         tfType,
			IsDataSource: isDataSource,
			Provider:     provider,
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
			"local",
			"localInput",
			"localInput1",
		}),
		Variables: variables([]string{
			"region",
			"name",
			"instance",
			"default_vpc",
			"local",
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

	names := assignNames(g, map[string]bool{}, true)

	assert.Equal(t, "vpcId", names[g.Outputs["vpc_id"]])
	assert.Equal(t, "securityGroupId", names[g.Outputs["security_group_id"]])
	assert.Equal(t, "name", names[g.Outputs["name"]])

	assert.Equal(t, "myVpcId", names[g.Locals["vpc_id"]])
	assert.Equal(t, "vpc", names[g.Locals["vpc"]])
	assert.Equal(t, "myName", names[g.Locals["name"]])
	assert.Equal(t, "ec2", names[g.Locals["ec2"]])
	assert.Equal(t, "local", names[g.Locals["local"]])
	assert.Equal(t, "localInput", names[g.Locals["localInput"]])
	assert.Equal(t, "localInput1", names[g.Locals["localInput1"]])

	assert.Equal(t, "region", names[g.Variables["region"]])
	assert.Equal(t, "nameInput", names[g.Variables["name"]])
	assert.Equal(t, "instance", names[g.Variables["instance"]])
	assert.Equal(t, "defaultVpc", names[g.Variables["default_vpc"]])
	assert.Equal(t, "localInput2", names[g.Variables["local"]])

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

func boundRef(v string, typ il.Type, node il.Node) *il.BoundVariableAccess {
	tfVar, err := config.NewInterpolatedVariable(v)
	contract.Assert(err == nil)

	switch tfVar := tfVar.(type) {
	case *config.ResourceVariable:
		return &il.BoundVariableAccess{
			Elements: strings.Split(tfVar.Field, "."),
			ExprType: typ,
			TFVar:    tfVar,
			ILNode:   node,
		}
	default:
		return &il.BoundVariableAccess{
			ExprType: typ,
			TFVar:    tfVar,
			ILNode:   node,
		}
	}
}

func applyArgRef(arg il.BoundExpr, index int) *il.BoundCall {
	return &il.BoundCall{
		Func:     "__applyArg",
		ExprType: arg.Type().ElementType(),
		Args:     []il.BoundExpr{&il.BoundLiteral{ExprType: il.TypeNumber, Value: index}},
	}
}

func TestAssignApplyNames(t *testing.T) {
	m := &il.Graph{
		Locals: locals([]string{
			"vpc",
			"name",
		}),
		Variables: variables([]string{
			"region",
			"name",
		}),
		Modules: modules([]string{
			"vpc",
			"module",
			"name",
		}),
		Resources: resources([]string{
			"aws:ec2:Vpc::default",
			"aws:ec2:Instance::default",
			"aws:ec2:getInstances::main",
		}),
	}

	g := &generator{nameTable: assignNames(m, map[string]bool{}, true)}

	args := []*il.BoundVariableAccess{
		boundRef("local.name", il.TypeString.OutputOf(), m.Locals["name"]),
		boundRef("var.name", il.TypeString.OutputOf(), m.Variables["name"]),
		boundRef("var.region", il.TypeString.OutputOf(), m.Variables["region"]),
		boundRef("module.vpc.foo", il.TypeString.OutputOf(), m.Modules["vpc"]),
		boundRef("module.vpc.foo", il.TypeString.OutputOf(), m.Modules["vpc"]),
		boundRef("module.vpc.bar.foo", il.TypeString.OutputOf(), m.Modules["vpc"]),
		boundRef("aws_ec2_vpc.default.name", il.TypeString.OutputOf(), m.Resources["aws:ec2:Vpc::default"]),
		boundRef("aws_ec2_instances.main.*.name", il.TypeString.OutputOf(), m.Resources["aws:ec2:getInstances::main"]),
		boundRef("aws_ec2_instance.default.vpc", il.TypeString.OutputOf(), m.Resources["aws:ec2:Instance::default"]),
		boundRef("aws_ec2_instance.default.vpc", il.TypeString.OutputOf(), m.Resources["aws:ec2:Instance::default"]),
		boundRef("aws_ec2_instance.default.vpc.x", il.TypeString.OutputOf(), m.Resources["aws:ec2:Instance::default"]),
		boundRef("aws_ec2_instance.default.x.vpc", il.TypeString.OutputOf(), m.Resources["aws:ec2:Instance::default"]),
	}
	then := &il.BoundOutput{
		Exprs: []il.BoundExpr{
			boundRef("local.vpc", il.TypeString, m.Locals["vpc"]),
		},
	}
	for i, arg := range args {
		then.Exprs = append(then.Exprs, applyArgRef(arg, i))
	}

	names := g.assignApplyArgNames(args, then)
	assert.Equal(t, "name", names[0])
	assert.Equal(t, "nameInput", names[1])
	assert.Equal(t, "region", names[2])
	assert.Equal(t, "vpcInstanceFoo", names[3])
	assert.Equal(t, "vpcInstanceFoo1", names[4])
	assert.Equal(t, "bar", names[5])
	assert.Equal(t, "defaultVpcName", names[6])
	assert.Equal(t, "main", names[7])
	assert.Equal(t, "defaultInstanceVpc", names[8])
	assert.Equal(t, "defaultInstanceVpc1", names[9])
	assert.Equal(t, "defaultInstanceVpc2", names[10])
	assert.Equal(t, "x", names[11])
}

package il

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/internal/config"
	"github.com/pulumi/tf2pulumi/internal/config/module"
)

func newLocal(t *testing.T, name, value string) *config.Local {
	raw, err := config.NewRawConfig(map[string]interface{}{
		"value": value,
	})
	if err != nil {
		t.Fatalf("NewRawConfig failed: %v", err)
	}
	return &config.Local{
		Name:      name,
		RawConfig: raw,
	}
}

func TestCircularLocals(t *testing.T) {
	cfg := &config.Config{
		Locals: []*config.Local{newLocal(t, "a", "${local.a}")},
	}
	tree := module.NewTree("test", cfg)

	_, err := BuildGraph(tree, nil)
	assert.Error(t, err)

	cfg.Locals = []*config.Local{
		newLocal(t, "a", "${local.b}"),
		newLocal(t, "b", "${local.a}"),
	}

	_, err = BuildGraph(tree, nil)
	assert.Error(t, err)
}

func TestLocalForwardReferences(t *testing.T) {
	cfg := &config.Config{
		Locals: []*config.Local{
			newLocal(t, "a", "${local.b}"),
			newLocal(t, "b", "foo"),
		},
	}
	tree := module.NewTree("test", cfg)

	_, err := BuildGraph(tree, nil)
	assert.NoError(t, err)
}

func TestMetaProperties(t *testing.T) {
	conf, err := config.LoadDir("testdata/test_meta_properties")
	if err != nil {
		t.Fatalf("could not load config: %v", err)
	}

	g, err := BuildGraph(module.NewTree("main", conf), &BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	r1, ok := g.Resources["aws_instance.r1"]
	assert.True(t, ok)
	assert.Equal(t, &BoundMapProperty{
		Elements: map[string]BoundNode{
			"create": &BoundLiteral{ExprType: TypeString, Value: "20m"},
			"update": &BoundLiteral{ExprType: TypeString, Value: "5m"},
			"delete": &BoundLiteral{ExprType: TypeString, Value: "1h"},
		},
	}, r1.Timeouts)

	r2, ok := g.Resources["aws_instance.r2"]
	assert.True(t, ok)
	assert.Equal(t, []string{
		"ami",
		"arn",
		"associatePublicIpAddress",
		"availabilityZone",
		"cpuCoreCount",
		"cpuThreadsPerCore",
		"creditSpecification",
		"disableApiTermination",
		"ebsBlockDevices",
		"ebsOptimized",
		"ephemeralBlockDevices",
		"getPasswordData",
		"hostId",
		"iamInstanceProfile",
		"instanceInitiatedShutdownBehavior",
		"instanceState",
		"instanceType",
		"ipv6AddressCount",
		"ipv6Addresses",
		"keyName",
		"monitoring",
		"networkInterfaceId",
		"networkInterfaces",
		"passwordData",
		"placementGroup",
		"primaryNetworkInterfaceId",
		"privateDns",
		"privateIp",
		"publicDns",
		"publicIp",
		"rootBlockDevice",
		"securityGroups",
		"sourceDestCheck",
		"subnetId",
		"tags",
		"tenancy",
		"userData",
		"userDataBase64",
		"volumeTags",
		"vpcSecurityGroupIds",
	}, r2.IgnoreChanges)

	r3, ok := g.Resources["aws_instance.r3"]
	assert.True(t, ok)
	assert.Equal(t, []string{
		"ami",
		"networkInterfaces[0].networkInterfaceId",
		"rootBlockDevice.encrypted",
		"tags.Creator",
		"userData",
		"userDataBase64",
	}, r3.IgnoreChanges)
}

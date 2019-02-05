package nodejs

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/stretchr/testify/assert"

	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

func TestLegalIdentifiers(t *testing.T) {
	legalIdentifiers := []string{
		"foobar",
		"$foobar",
		"_foobar",
		"_foo$bar",
		"_foo1bar",
		"Foobar",
	}
	for _, id := range legalIdentifiers {
		assert.True(t, isLegalIdentifier(id))
		assert.Equal(t, id, cleanName(id))
	}

	type illegalCase struct {
		original string
		expected string
	}
	illegalCases := []illegalCase{
		{"123foo", "_123foo"},
		{"foo.bar", "foo_bar"},
		{"$foo/bar", "$foo_bar"},
		{"12/bar\\baz", "_12_bar_baz"},
		{"foo bar", "foo_bar"},
		{"foo-bar", "foo_bar"},
		{".bar", "_bar"},
		{"1.bar", "_1_bar"},
	}
	for _, c := range illegalCases {
		assert.False(t, isLegalIdentifier(c.original))
		assert.Equal(t, c.expected, cleanName(c.original))
	}
}

func TestLowerToLiteral(t *testing.T) {
	prop := &il.BoundMapProperty{
		Elements: map[string]il.BoundNode{
			"key": &il.BoundOutput{
				HILNode: nil,
				Exprs: []il.BoundExpr{
					&il.BoundLiteral{
						ExprType: il.TypeString,
						Value:    "module: ",
					},
					&il.BoundVariableAccess{
						ExprType: il.TypeString,
						TFVar:    &config.PathVariable{Type: config.PathValueModule},
					},
					&il.BoundLiteral{
						ExprType: il.TypeString,
						Value:    " root: ",
					},
					&il.BoundVariableAccess{
						ExprType: il.TypeString,
						TFVar:    &config.PathVariable{Type: config.PathValueRoot},
					},
				},
			},
		},
	}

	g := &generator{
		rootPath: ".",
		module:   &il.Graph{Tree: module.NewTree("foo", &config.Config{Dir: "./foo/bar"})},
	}

	p, err := g.lowerToLiterals(prop)
	assert.NoError(t, err)

	pmap := p.(*il.BoundMapProperty)
	pout := pmap.Elements["key"].(*il.BoundOutput)

	lit1, ok := pout.Exprs[1].(*il.BoundLiteral)
	assert.True(t, ok)
	assert.Equal(t, "foo/bar", lit1.Value)

	lit3, ok := pout.Exprs[3].(*il.BoundLiteral)
	assert.True(t, ok)
	assert.Equal(t, ".", lit3.Value)

	computed, _, err := g.computeProperty(prop, false, "")
	assert.NoError(t, err)
	assert.Equal(t, "{\n    key: `module: foo/bar root: .`,\n}", computed)
}

func TestComments(t *testing.T) {
	const hclText = `
# Accept the AWS region as input.
variable "aws_region" {
	# Default to us-west-2
	default = "us-west-2"
}

/*
Specify provider details
*/
provider "aws" {
	# Pull the region from a variable
    region = "${var.aws_region}"
}

# Create a VPC.
#
# Note that the VPC has been tagged appropriately.
resource "aws_vpc" "default" {
    cidr_block = "10.0.0.0/16"  # Just one CIDR block
	enable_dns_hostnames = true # Definitely want DNS hostnames.

	# The tag collection for this VPC.
	tags {
		# Ensure that we tag this VPC with a Name.
		Name = "test"
	}
}

locals {
	# The VPC details
	vpc = {
		# The ID
		id = "${aws_vpc.default.id}"
	}

	# The region, again
	region = "${var.aws_region}" // why not
}

// Create a security group.
//
// This group should allow SSH and HTTP access.
resource "aws_security_group" "default" {
	vpc_id = "${locals.vpc_id.id}"

	// SSH access from anywhere
	ingress {
		from_port   = 22
		to_port     = 22
		protocol    = "tcp"
		// "0.0.0.0/0" is anywhere
		cidr_blocks = ["0.0.0.0/0"]
	}

	// HTTP access from anywhere
	ingress {
		from_port   = 80
		to_port     = 80
		protocol    = "tcp" /* HTTP is TCP-only */
		cidr_blocks = ["0.0.0.0/0"]
	}

	// outbound internet access
	egress {
		from_port   = 0
		to_port     = 0
		protocol    = "-1" // All
		cidr_blocks = ["0.0.0.0/0"]
	}
}

/**
 * Output the SG name.
 *
 * We pull the name from the default SG.
 */
output "security_group_name" {
	/* Take the value from the default SG. */
	value = "${aws_security_group.default.name}" # Neat!
}
`

	const expectedText = `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const config = new pulumi.Config();
// Accept the AWS region as input.
const var_aws_region = config.get("awsRegion") || "us-west-2";

// Create a security group.
//
// This group should allow SSH and HTTP access.
const aws_security_group_default = new aws.ec2.SecurityGroup("default", {
    // outbound internet access
    egress: [{
        cidrBlocks: ["0.0.0.0/0"],
        fromPort: 0,
        protocol: "-1", // All
        toPort: 0,
    }],
    ingress: [
        // SSH access from anywhere
        {
            // "0.0.0.0/0" is anywhere
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 22,
            protocol: "tcp",
            toPort: 22,
        },
        // HTTP access from anywhere
        {
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 80,
            protocol: "tcp", // HTTP is TCP-only
            toPort: 80,
        },
    ],
    vpcId: locals_vpc_id.id,
});
// Create a VPC.
//
// Note that the VPC has been tagged appropriately.
const aws_vpc_default = new aws.ec2.Vpc("default", {
    cidrBlock: "10.0.0.0/16", // Just one CIDR block
    enableDnsHostnames: true, // Definitely want DNS hostnames.
    // The tag collection for this VPC.
    tags: {
        // Ensure that we tag this VPC with a Name.
        Name: "test",
    },
});
// The region, again
const local_region = var_aws_region; // why not
// The VPC details
const local_vpc = [{
    // The ID
    id: aws_vpc_default.id,
}];

// Output the SG name.
//
// We pull the name from the default SG.
// Take the value from the default SG.
export const securityGroupName = aws_security_group_default.name; // Neat!
`

	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("could not create temporary directory: %v", err)
	}
	defer func() {
		contract.IgnoreError(os.RemoveAll(dir))
	}()

	err = ioutil.WriteFile(path.Join(dir, "main.tf"), []byte(hclText), 0600)
	if err != nil {
		t.Fatalf("could not create main.tf: %v", err)
	}

	conf, err := config.LoadDir(dir)
	if err != nil {
		t.Fatalf("could not load config: %v", err)
	}

	g, err := il.BuildGraph(module.NewTree("main", conf), &il.BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	if err != nil {
		t.Fatalf("could not build graph: %v", err)
	}

	var b bytes.Buffer
	err = gen.Generate([]*il.Graph{g}, New("main", &b))
	assert.NoError(t, err)

	assert.Equal(t, expectedText, b.String())
}

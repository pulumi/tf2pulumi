package il

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/stretchr/testify/assert"
)

func assertLeading(t *testing.T, c *Comments, expected ...string) {
	assert.Equal(t, expected, c.Leading)
}

func TestExtractComments(t *testing.T) {
	const hclText = `
# Accept the AWS region as input.
variable "aws_region" {
	# Default to us-west-2
	default = "us-west-2"
}

# Specify provider details
provider "aws" {
	# Pull the region from a variable
    region = "${var.aws_region}"
}

# Create a VPC.
#
# Note that the VPC has been tagged appropriately.
resource "aws_vpc" "default" {
    cidr_block = "10.0.0.0/16"
	enable_dns_hostnames = true

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
		protocol    = "tcp"
		cidr_blocks = ["0.0.0.0/0"]
	}

	// outbound internet access
	egress {
		from_port   = 0
		to_port     = 0
		protocol    = "-1"
		cidr_blocks = ["0.0.0.0/0"]
	}
}

# Output the SG name.
output "security_group_name" {
	# Take the value from the default SG.
	value = "${aws_security_group.default.name}"
}
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

	b := newBuilder(&BuildOptions{
		AllowMissingProviders: true,
		AllowMissingVariables: true,
		AllowMissingComments:  true,
	})
	err = b.buildNodes(conf)
	assert.NoError(t, err)

	err = b.extractComments(conf)
	assert.NoError(t, err)

	v := b.variables["aws_region"]
	assertLeading(t, v.Comments, " Accept the AWS region as input.")
	assertLeading(t, v.DefaultValue.Comments(), " Default to us-west-2")

	p := b.providers["aws"]
	assertLeading(t, p.Comments, " Specify provider details")
	assertLeading(t, p.Properties.Elements["region"].Comments(), " Pull the region from a variable")

	l := b.locals["vpc"]
	assertLeading(t, l.Comments, " The VPC details")
	lval := l.Value.(*BoundListProperty).Elements[0].(*BoundMapProperty)
	assertLeading(t, lval.Elements["id"].Comments(), " The ID")

	vpc := b.resources["aws_vpc.default"]
	assertLeading(t, vpc.Comments, " Create a VPC.", "", " Note that the VPC has been tagged appropriately.")
	tagsProp := vpc.Properties.Elements["tags"].(*BoundMapProperty)
	assertLeading(t, tagsProp.Comments(), " The tag collection for this VPC.")
	assertLeading(t, tagsProp.Elements["Name"].Comments(), " Ensure that we tag this VPC with a Name.")

	sg := b.resources["aws_security_group.default"]
	assertLeading(t, sg.Comments, " Create a security group.", "", " This group should allow SSH and HTTP access.")
	ingressList := sg.Properties.Elements["ingress"].(*BoundListProperty)
	sshAccess := ingressList.Elements[0].(*BoundMapProperty)
	assertLeading(t, sshAccess.Comments(), " SSH access from anywhere")
	assertLeading(t, sshAccess.Elements["cidr_blocks"].Comments(), ` "0.0.0.0/0" is anywhere`)
	assertLeading(t, ingressList.Elements[1].Comments(), " HTTP access from anywhere")
	egressList := sg.Properties.Elements["egress"].(*BoundListProperty)
	assertLeading(t, egressList.Comments(), " outbound internet access")

	out := b.outputs["security_group_name"]
	assertLeading(t, out.Comments, " Output the SG name.")
	assertLeading(t, out.Value.Comments(), " Take the value from the default SG.")
}

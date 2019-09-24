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

# Use some data sources.
data "aws_subnet_ids" "default" {
	vpc_id = "${aws_vpc.default.id}"
}

data "aws_availability_zones" "default" {}

data "aws_availability_zone" "default" {
	count = "${length(data.aws_availability_zones.default.ids)}"
	zone_id = "${data.aws_availability_zones.default.zone_ids[count.index]}"
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
	vpc_id = "${local.vpc["id"]}"

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

	tags {
		Vpc = "VPC ${var.aws_region}:${aws_vpc.default.id}"
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

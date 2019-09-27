variable "create_sg" {
  default = false
}

# Accept the AWS region as input.
variable "aws_region" {
  # Default to us-west-2
  default = "us-west-2"
}

locals {
  in_us_east_1 = "${var.aws_region == "us-east-1"}"
}

# Optionally create a security group and attach some rules.
resource "aws_security_group" "default" {
  count = "${var.create_sg ? 1 : 0}"

  description = "Default security group"
}

# SSH access from anywhere
resource "aws_security_group_rule" "ingress" {
  count = "${var.create_sg ? 1 : 0}"

  type = "ingress"
  from_port   = 22
  to_port     = 22
  protocol    = "tcp"
  cidr_blocks = ["0.0.0.0/0"]

  security_group_id = "${aws_security_group.default.id}"
}

# outbound internet access
resource "aws_security_group_rule" "egress" {
  count = "${var.create_sg ? 1 : 0}"

  type = "ingress"
  from_port   = 0
  to_port     = 0
  protocol    = "-1"
  cidr_blocks = ["0.0.0.0/0"]

  security_group_id = "${aws_security_group.default.id}"
}

# If we are in us-east-1, create an ec2 instance
resource "aws_instance" "web" {
  count = "${local.in_us_east_1}"

  ami           = "some-ami"
  instance_type = "t2.micro"

  tags = {
    Name = "HelloWorld"
  }
}

# If we are in us-east-2, create a different ec2 instance
resource "aws_instance" "web2" {
  count = "${var.aws_region == "us-east-2" ? 1 : 0}"

  ami = "some-other-ami"
  instance_type = "t2.micro"

  tags = {
    Name = "instance-${count.index % 2}"
  }
}

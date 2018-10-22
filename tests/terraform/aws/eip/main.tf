# NOTE: we do not specify names for any of the test resources in order to improve the reliability of our CI jobs
# in the face of parallelism and leftover resources. Explicitly naming these resources can cause conflicts
# between jobs that run concurrently or jobs that fail to clean up their resources. Pulumi will auto-name these
# for us.

# Specify the provider and access details
provider "aws" {
  region = "${var.aws_region}"
}

resource "aws_eip" "default" {
  instance = "${aws_instance.web.id}"
  vpc      = true
}

# Our default security group to access
# the instances over SSH and HTTP
resource "aws_security_group" "default" {
  # name        = "eip_example"
  description = "Used in the terraform"

  # SSH access from anywhere
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTP access from anywhere
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # outbound internet access
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "web" {
  instance_type = "t2.micro"

  # Lookup the correct AMI based on the region
  # we specified
  ami = "${lookup(var.aws_amis, var.aws_region)}"

  # Our Security group to allow HTTP and SSH access
  security_groups = ["${aws_security_group.default.name}"]

  # We run a remote provisioner on the instance after creating it.
  # In this case, we just install nginx and start it. By default,
  # this should be on port 80
  user_data = "${file("userdata.sh")}"

  #Instance tags
  tags {
    Name = "eip-example"
  }
}

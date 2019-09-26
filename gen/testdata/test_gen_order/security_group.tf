# NOTE: we do not specify names for any of the test resources in order to improve the reliability of our CI jobs
# in the face of parallelism and leftover resources. Explicitly naming these resources can cause conflicts
# between jobs that run concurrently or jobs that fail to clean up their resources. Pulumi will auto-name these
# for us.

resource "aws_security_group" "az" {
  # name        = "az-${data.aws_availability_zone.target.name}"
  description = "Open access within the AZ ${data.aws_availability_zone.target.name}"
  vpc_id      = "${var.vpc_id}"

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = -1
    cidr_blocks = ["${aws_subnet.main.cidr_block}"]
  }
}

output "security_group_id" {
  value = "${aws_security_group.az.id}"
}

# NOTE: we do not specify names for any of the test resources in order to improve the reliability of our CI jobs
# in the face of parallelism and leftover resources. Explicitly naming these resources can cause conflicts
# between jobs that run concurrently or jobs that fail to clean up their resources. Pulumi will auto-name these
# for us.

resource "aws_security_group" "region" {
  # name        = "region"
  description = "Open access within this region"
  vpc_id      = "${aws_vpc.main.id}"

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = -1
    cidr_blocks = ["${aws_vpc.main.cidr_block}"]
  }
}

resource "aws_security_group" "internal-all" {
  # name        = "internal-all"
  description = "Open access within the full internal network"
  vpc_id      = "${aws_vpc.main.id}"

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = -1
    cidr_blocks = ["${var.base_cidr_block}"]
  }
}

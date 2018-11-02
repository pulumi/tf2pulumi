variable "availability_zones" {
  default = "us-east-1a,us-east-1b"
}
variable "public_key" {
  default = ""
}

locals  {
  az_list = ["${split(",", var.availability_zones)}"]
}

resource "aws_elb" "elb" {
  name_prefix = "webelb"

  availability_zones = ["${local.az_list}"]

  listener {
    instance_port     = 80
    instance_protocol = "http"
    lb_port           = 80
    lb_protocol       = "http"
  }

  health_check {
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 3
    target              = "HTTP:80/"
    interval            = 30
  }
}

data "aws_ami" "ubuntu" {
  most_recent = true

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  owners = ["099720109477"] # Canonical
}

resource "aws_security_group" "default" {
  name_prefix = "example_sg"

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

resource "aws_key_pair" "default" {
  count = "${var.public_key == "" ? 0 : 1}"

  key_name_prefix = "default"
  public_key = "${var.public_key}"
}

resource "aws_security_group_rule" "allow_ssh" {
  count = "${var.public_key == "" ? 0 : 1}"

  type = "ingress"
  from_port = 22
  to_port = 22
  protocol = "tcp"
  cidr_blocks = ["0.0.0.0/0"]

  security_group_id = "${aws_security_group.default.id}"
}

resource "aws_instance" "web-server-key" {
  count = "${var.public_key == "" ? 0 : length(local.az_list)}"

  associate_public_ip_address = true

  ami = "${data.aws_ami.ubuntu.id}"
  instance_type = "t2.micro"

  availability_zone = "${element(local.az_list, count.index)}"
  user_data = "${file("userdata.sh")}"
  security_groups = ["${aws_security_group.default.name}"]

  key_name = "${aws_key_pair.default.0.key_name}"
}

resource "aws_elb_attachment" "web-server-key" {
  count = "${var.public_key == "" ? 0 : length(local.az_list)}"

  elb = "${aws_elb.elb.id}"
  instance = "${element(aws_instance.web-server-key.*.id, count.index)}"
}

resource "aws_instance" "web-server-nokey" {
  count = "${var.public_key == "" ? length(local.az_list) : 0}"

  ami = "${data.aws_ami.ubuntu.id}"
  instance_type = "t2.micro"

  availability_zone = "${element(local.az_list, count.index)}"
  user_data = "${file("userdata.sh")}"
  security_groups = ["${aws_security_group.default.name}"]
}

resource "aws_elb_attachment" "web-server-nokey" {
  count = "${var.public_key == "" ? length(local.az_list) : 0}"

  elb = "${aws_elb.elb.id}"
  instance = "${element(aws_instance.web-server-nokey.*.id, count.index)}"
}

output "url" {
  value = "http://${aws_elb.elb.dns_name}"
}

output "public_ips" {
  value = "${aws_instance.web-server-key.*.public_ip}"
}

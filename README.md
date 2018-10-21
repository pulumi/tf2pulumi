# tf2pulumi

[![Build Status](https://travis-ci.com/pulumi/tf2pulumi.svg?branch=master)](https://travis-ci.com/pulumi/tf2pulumi)

Convert Terraform projects to Pulumi TypeScript programs.

## Goals

The goal of `tf2pulumi` is to help users efficiently convert Terraform-managed infrastructure into
Pulumi stacks. It translates HCL configuration into Pulumi TypeScript programs. In the fullness of
time, it will also translate Terraform state files into Pulumi checkpoint files.

## Installation

You need to have [Go](https://golang.org/) and [`dep`](https://github.com/golang/dep) installed in
order to build `tf2pulumi`. Once those prerequisites are installed, run the following to build the
`tf2pulumi` binary and install it into `$GOPATH/bin`:

```console
$ go get github.com/pulumi/tf2pulumi/...
$ cd "$(go env GOPATH)/src/github.com/pulumi/tf2pulumi
$ dep ensure
$ go install github.com/pulumi/tf2pulumi
```

If `$GOPATH/bin` is not on your path, you may want to move the `tf2pulumi` binary from `$GOPATH/bin`
into a directory that is on your path.

## Usage

In order to use `tf2pulumi` to convert a Terraform project to Pulumi TypeScript and then deploy it,
you'll first need to install the [Pulumi CLI](https://pulumi.io/quickstart/install.html). Once the
Pulumi CLI has been installed, navigate to the same directory as the Terraform project you'd like to
import and create a new Pulumi TypeScript stack in a subdirectory:

```console
$ pulumi new typescript --dir my-stack
```

Then run `tf2pulumi` and redirect its output to a file named `index.ts` in the directory that
contains the Pulumi project you just created:

```console
$ tf2pulumi >my-stack/index.ts
```

If `tf2pulumi` complains about missing Terraform resource plugins, install those plugins as per the
instructions in the error message and re-run the command above.

This will generate a Pulumi TypeScript program in `index.ts` that when run with `pulumi update` will deploy the
infrastructure originally described by the Terraform project. Note that if your infrastructure
references files or directories with paths relative to the location of the Terraform project, you
will most likely need to update these paths such that they are relative to the generated `index.ts`
file.

## Limitations

While the majority of Terraform constructs are already supported, there are some gaps. Most
noteworthy is that `tf2pulumi` cannot currently convert provisioner blocks. Besides that, the
following constructs are not yet implemented:
- Various built-in interpolation functions. Calls to unimplemented functions will throw at
  runtime (#12).
- Multiple provider instances and named providers. If a resource or data source names a provider,
  the reference is currently ignored (#11).
- Provisioners. Provisioner blocks are currently ignored (#10).
- Explicit dependencies in data sources (#1).
- `self` and `terraform` variable references (#2).

## Example

The [AWS EIP test](https://github.com/pulumi/tf2pulumi/tree/master/tests/terraform/aws/eip) provides
a good example of the results of the conversion on a relatively simple infrastructure definition. 

### Terraform

The Terraform project is structured like so:

##### variables.tf
```terraform
variable "aws_region" {
  description = "The AWS region to create things in."
  default     = "us-east-1"
}

# Amazon Linux 2018.03
variable "aws_amis" {
  default = {
    "us-east-1" = "ami-0ff8a91507f77f867"
    "us-west-2" = "ami-a0cfeed8"
  }
}
```

##### main.tf
```terraform
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
  name        = "eip_example"
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
```

##### outputs.tf
```
output "address" {
  value = "${aws_instance.web.private_ip}"
}

output "elastic ip" {
  value = "${aws_eip.default.public_ip}"
}
```

### Pulumi

Running `tf2pulumi` on this project produces the following `index.ts` file:

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";
import * as fs from "fs";
import * as process from "process";
import sprintf = require("sprintf-js");

const config = new pulumi.Config();
const var_aws_amis = config.get("awsAmis") || {
    "us-east-1": "ami-0ff8a91507f77f867",
    "us-west-2": "ami-a0cfeed8",
};
const var_aws_region = config.get("awsRegion") || "us-east-1";

const aws_security_group_default = new aws.ec2.SecurityGroup("default", {
    description: "Used in the terraform",
    egress: [
        {
            cidrBlocks: [
                "0.0.0.0/0",
            ],
            fromPort: 0,
            protocol: "-1",
            toPort: 0,
        },
    ],
    ingress: [
        {
            cidrBlocks: [
                "0.0.0.0/0",
            ],
            fromPort: 22,
            protocol: "tcp",
            toPort: 22,
        },
        {
            cidrBlocks: [
                "0.0.0.0/0",
            ],
            fromPort: 80,
            protocol: "tcp",
            toPort: 80,
        },
    ],
    name: "eip_example",
});
const aws_instance_web = new aws.ec2.Instance("web", {
    ami: (<any>var_aws_amis)[var_aws_region],
    instanceType: "t2.micro",
    securityGroups: [
        aws_security_group_default.name,
    ],
    tags: {
        Name: "eip-example",
    },
    userData: fs.readFileSync("userdata.sh", "utf-8"),
});
const aws_eip_default = new aws.ec2.Eip("default", {
    instance: aws_instance_web.id,
    vpc: true,
});

export const address = aws_instance_web.privateIp;
export const elastic_ip = aws_eip_default.publicIp;
```

The Terraform variables have been converted to Pulumi config values, the resources to calls to the
appropriate Pulumi resource constructors, and the outputs to `export` statements.

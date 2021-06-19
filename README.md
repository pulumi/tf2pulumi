# tf2pulumi

[![Build Status](https://travis-ci.com/pulumi/tf2pulumi.svg?branch=master)](https://travis-ci.com/pulumi/tf2pulumi)

Convert Terraform projects to Pulumi TypeScript programs.

## Goals

The goal of `tf2pulumi` is to help users efficiently convert Terraform-managed infrastructure into
Pulumi stacks. It translates HCL configuration into Pulumi TypeScript programs. In the fullness of
time, it will also translate Terraform state files into Pulumi checkpoint files.

## Building and Installation

If you wish to use `tf2pulumi` without developing the tool itself, you can use one of the [binary
releases](https://github.com/pulumi/tf2pulumi/releases) hosted on GitHub.

### Homebrew
`tf2pulumi` can be installed on Mac from the Pulumi Homebrew tap.
```console
brew install pulumi/tap/tf2pulumi
```

tf2pulumi uses [Go modules](https://github.com/golang/go/wiki/Modules) to manage dependencies. If you want to develop `tf2pulumi2` itself, you'll need to have [Go](https://golang.org/)  installed in order to build.
Once this prerequisite is installed, run the following to build the `tf2pulumi` binary and install it into `$GOPATH/bin`:

```console
$ go build -o $GOPATH/bin/tf2pulumi main.go
```

Go should automatically handle pulling the dependencies for you.

If `$GOPATH/bin` is not on your path, you may want to move the `tf2pulumi` binary from `$GOPATH/bin`
into a directory that is on your path.

## Usage

In order to use `tf2pulumi` to convert a Terraform project to Pulumi and then deploy it,
you'll first need to install the [Pulumi CLI](https://pulumi.io/quickstart/install.html). Once the
Pulumi CLI has been installed, navigate to the same directory as the Terraform project you'd like to
import and create a new Pulumi stack in your favourite language:

```console
// For a Go project
$ pulumi new go -f

// For a TypeScript project
$ pulumi new typescript -f

// For a Python project
$ pulumi new python -f

// For a C# project
$ pulumi new csharp -f
```

Then run `tf2pulumi` which will write a file in the directory that
contains the Pulumi project you just created:

```console
// For a Go project
$ tf2pulumi --target-language go 

// For a TypeScript project
$ tf2pulumi --target-language typescript

// For a Python project
$ tf2pulumi --target-language python

// For a C# project
$ tf2pulumi --target-language csharp
```

By default, the conversion output will be stored in the current working directory. To override
this, use the `--output` (`-o` for short) flag.

If `tf2pulumi` complains about missing Terraform resource plugins, install those plugins as per the
instructions in the error message and re-run the command above.

This will generate a Pulumi TypeScript program that when run with `pulumi update` will deploy the
infrastructure originally described by the Terraform project. Note that if your infrastructure
references files or directories with paths relative to the location of the Terraform project, you
will most likely need to update these paths such that they are relative to the generated file.

## Adopting Resource From TFState

If you would like to adopt resources from an existing `.tfstate` file under management of a Pulumi
stack, copy the [`import.ts`](./misc/import/import.ts) file from this repo into your project folder,
and add the following in your generated `index.ts` file before any resource creations:

```ts
...
import "./import";
...
```

Then set the `importFromStatefile` config setting on your project to a valid location of a
`.tfstate` file to import resources from that state.

```
pulumi config set importFromStatefile ./terraform.tfstate
```

After doing this, the first `pulumi up` for a new stack with this configuration variable set will
`import` instead of `create` all of the resources defined in the code. Once imported, the existing
resources in your cloud provider can now be managed by Pulumi going forward.  See the [Adopting
Existing Cloud Resources into
Pulumi](https://www.pulumi.com/blog/adopting-existing-cloud-resources-into-pulumi/) blog post for
more details on importing existing resources.

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

const config = new pulumi.Config();
const awsRegion = config.get("awsRegion") || "us-east-1";
// Amazon Linux 2018.03
const awsAmis = config.get("awsAmis") || {
    "us-east-1": "ami-0ff8a91507f77f867",
    "us-west-2": "ami-a0cfeed8",
};

// Our default security group to access
// the instances over SSH and HTTP
const defaultSecurityGroup = new aws.ec2.SecurityGroup("default", {
    description: "Used in the terraform",
    // outbound internet access
    egress: [{
        cidrBlocks: ["0.0.0.0/0"],
        fromPort: 0,
        protocol: "-1",
        toPort: 0,
    }],
    ingress: [
        // SSH access from anywhere
        {
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 22,
            protocol: "tcp",
            toPort: 22,
        },
        // HTTP access from anywhere
        {
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 80,
            protocol: "tcp",
            toPort: 80,
        },
    ],
    name: "eip_example",
});
const web = new aws.ec2.Instance("web", {
    // Lookup the correct AMI based on the region
    // we specified
    ami: (<any>awsAmis)[awsRegion],
    instanceType: "t2.micro",
    // Our Security group to allow HTTP and SSH access
    securityGroups: [defaultSecurityGroup.name],
    //Instance tags
    tags: {
        Name: "eip-example",
    },
    // We run a remote provisioner on the instance after creating it.
    // In this case, we just install nginx and start it. By default,
    // this should be on port 80
    userData: fs.readFileSync("userdata.sh", "utf-8"),
});
const defaultEip = new aws.ec2.Eip("default", {
    instance: web.id,
    vpc: true,
});

export const address = web.privateIp;
export const elastic_ip = defaultEip.publicIp;
```

The Terraform variables have been converted to Pulumi config values, the resources to calls to the
appropriate Pulumi resource constructors, and the outputs to `export` statements.

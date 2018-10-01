# tf2Pulumi

[![Travis CI](https://img.shields.io/travis/pulumi/tf2pulumi.svg?style=for-the-badge)](https://travis-ci.org/pulumi/tf2pulumi)

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
Pulumi CLI has been installed, navigate the to same directory as the Terraform project you'd like to
import and create a new Pulumi TypeScript stack in a subdirectory:

```console
$ pulumi new typescript --dir my-stack
```

Then run `tf2pulumi` and redirect its output to a file named `index.ts` in the directory that
contains the Pulumi project you just created:

```console
$ tf2pulumi >my-stack/index.ts
```

If `tf2pulumi` complains about missing resource plugins, install those plugins as per the
instructions in the error message and re-run the command above.

This will generate a Pulumi TypeScript program in `index.ts` that when run with the  will deploy the
infrastructure originally described by the Terraform project. Note that if your infrastructure
references files or directories with paths relative to the location of the Terraform project, you
will most likely need to update these paths such that they are relative to the generated `index.ts`
file.

## Limitations

While the majority of Terraform constructs are already supported, there are some gaps. Most
noteworthy is that `tf2pulumi` cannot currently convert provisioner blocks. Besides that, the
following constructs are not yet implemented:
- Various built-in interpolation functions. Calls to unimplemented functions will throw at
  runtime.
- Multiple provider instances and named providers. If a resource or data source names a provider,
  the reference is currently ignored.
- Provisioners. Provisioner blocks are currently ignored.
- Explicit dependencies in data sources.
- `self` and `terraform` variable references.

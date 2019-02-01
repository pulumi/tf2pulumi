# Contributing to tf2pulumi

First, thanks for contributing to tf2pulumi and helping make it better. We appreciate the help! If you're looking for an issue to start with, we've tagged some issues with the [help-wanted](https://github.com/pulumi/tf2pulumi/issues?q=is%3Aopen+is%3Aissue+label%3A%22help+wanted%22) tag but feel free to pick up any issue that looks interesting to you or fix a bug you stumble across in the course of using tf2pulumi. No matter the size, we welcome all improvements.

For larger features, we'd appreciate it if you open a [new issue](https://github.com/pulumi/tf2pulumi/issues/new) before doing a ton of work to discuss the feature before you start writing a lot of code.

## Hacking on tf2pulumi

To hack on tf2pulumi, you'll need to get a development environment set up. This is documented in the [README](https://github.com/pulumi/tf2pulumi/blob/master/README.md#installation).

## Make build system

We use `make` as our build system, so you'll want to install that as well, if you don't have it already. We have extremely limited support for doing development on Windows (the bare minimum for us to get Windows validation of `pulumi`) so if you're on windows, we recommend that you use the [Windows Subsystem for Linux](https://docs.microsoft.com/en-us/windows/wsl/install-win10).

Across our projects, we try to use a regular set of make targets. The ones you'll care most about are:

1. `make`, which builds tf2pulumi and runs a quick set of tests
2. `make all` which builds tf2pulumi and runs the quick tests and a larger set of tests.

We make heavy use of integration level testing where we invoke `pulumi` to create and then delete cloud resources. This requires you to have a Pulumi account (so [sign up for free](https://pulumi.com) today if you haven't already) and login with `pulumi login`.

Pulumi integration tests make use of the Go test runner. When using Go 1.10 or above, we recommend setting the `GOCACHE` environment variable to `off` to avoid
erroneously caching test results.

## Submitting a Pull Request

For contributors we use the standard fork based workflow. Fork this repository, create a topic branch, and start hacking away.  When you're ready, make sure you've run the tests (`make travis_pull_request` will run the exact flow we run in CI) and open your PR.

## Getting Help

We're sure there are rough edges and we appreciate you helping out. If you want to talk with other folks hacking on Pulumi (or members of the Pulumi team!) come hang out `#contribute` channel in the [Pulumi Community Slack](https://slack.pulumi.io/).

## Building Release Artifacts

To build the `.tar.gz` files that make up a tf2pulumi release, run `make release`. This will produce two releasable artifacts:

- tf2pulumi-linux-x64-vX.X.X.tar.gz
- tf2pulumi-darwin-x64-vX.X.X.tar.gz

These archives can then be uploaded as part of a GitHub Release.

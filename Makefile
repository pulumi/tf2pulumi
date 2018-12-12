PROJECT_NAME := Terraform -> Pulumi converter
include build/common.mk

VERSION := $(shell scripts/get-version)

# NOTE: Since the plugin is published using the nodejs style semver version
# We set the PLUGIN_VERSION to be the same as the version we use when building
# the provider (e.g. x.y.z-dev-... instead of x.y.zdev...)
build::
	go install -ldflags "-X github.com/tf2pulumi.Version=${VERSION}" github.com/pulumi/tf2pulumi

lint::
	golangci-lint run

test_all::
	PATH=$(PULUMI_BIN):$(PATH) go test -v -cover ./il/... ./gen/...
	PATH=$(PULUMI_BIN):$(PATH) go test -v -cover -timeout 1h ./tests/...

# The travis_* targets are entrypoints for CI.
.PHONY: travis_cron travis_push travis_pull_request travis_api
travis_cron: all
travis_push: all
travis_pull_request: all
travis_api: all

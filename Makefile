PROJECT_NAME := Terraform -> Pulumi converter

VERSION := $(shell pulumictl get version)
TESTPARALLELISM := 1
WORKING_DIR     := $(shell pwd)

build::
	go build -a -o ${WORKING_DIR}/bin/tf2pulumi -ldflags "-X github.com/pulumi/tf2pulumi/version.Version=${VERSION}" github.com/pulumi/tf2pulumi

lint::
	golangci-lint run --timeout 5m

test_acceptance::
	go test -v -count=1 -cover -timeout 2h -parallel ${TESTPARALLELISM} ./tests/...

install_plugins::
	[ -x $(shell which pulumi) ] || curl -fsSL https://get.pulumi.com | sh
	pulumi plugin install resource aws 2.0.0
	pulumi plugin install resource azure 2.0.0
	pulumi plugin install resource gcp 2.0.0
	pulumi plugin install resource terraform-template 0.16.0
	pulumi plugin install resource random 2.0.0

dev:: build lint test_acceptance

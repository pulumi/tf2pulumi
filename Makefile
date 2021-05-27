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

tf2pulumi_coverage_report::
	# (cd pkg/tf2pulumi/testdata && if [ ! -d terraform-gudies ]; then git clone https://github.com/hashicorp/terraform-provider-aws && cd azure-quickstart-templates && git checkout 3b2757465c2de537e333f5e2d1c3776c349b8483; fi)
	(cd tests/coverage-report/testdata && if [ ! -d terraform-provider-aws ]; then git clone https://github.com/hashicorp/terraform-provider-aws && cd terraform-provider-aws && git checkout 59d66d6283496aa47e90ec78d0eb3851e0a640e1; fi)
	(cd tests/coverage-report/testdata && if [ ! -d example-snippets ]; then cd ../test && go test -v -tags=coverage -run TestGenInput; fi)
	(cd tests/coverage-report/test && go test -v -tags=coverage -run TestTemplateCoverage)

install_plugins::
	[ -x $(shell which pulumi) ] || curl -fsSL https://get.pulumi.com | sh
	pulumi plugin install resource aws 2.0.0
	pulumi plugin install resource azure 2.0.0
	pulumi plugin install resource gcp 2.0.0
	pulumi plugin install resource terraform-template 0.16.0
	pulumi plugin install resource random 2.0.0

dev:: build lint test_acceptance

PROJECT_NAME := Terraform -> Pulumi converter

VERSION := $(shell pulumictl get version)
TESTPARALLELISM := 1
WORKING_DIR     := $(shell pwd)

build::
	go build -a -o ${WORKING_DIR}/bin/tf2pulumi -ldflags "-X github.com/pulumi/tf2pulumi/version.Version=${VERSION}" github.com/pulumi/tf2pulumi

lint::
	golangci-lint run --timeout 5m

test_acceptance::
	make generate_tf2pulumi_coverage_input
	go test -v -count=1 -cover -timeout 2h -parallel ${TESTPARALLELISM} ./tests/...

generate_tf2pulumi_coverage_input::
	(cd tests/coverage-report/testdata && if [ ! -d terraform-provider-aws ]; then git clone https://github.com/hashicorp/terraform-provider-aws && cd terraform-provider-aws && git checkout 59d66d6283496aa47e90ec78d0eb3851e0a640e1; fi)
	(cd tests/coverage-report/testdata && if [ ! -d terraform-provider-azurerm ]; then git clone https://github.com/terraform-providers/terraform-provider-azurerm && cd terraform-provider-azurerm && git checkout 8fc7613206855de21b1771a209510890f701a24b; fi)
	(cd tests/coverage-report/testdata && if [ ! -d terraform-provider-google ]; then git clone https://github.com/hashicorp/terraform-provider-google && cd terraform-provider-google && git checkout ce331bb9c3984301ac3e7132a49aebb93e656832; fi)
	(cd tests/coverage-report/testdata && if [ ! -d example-snippets ]; then cd ../test && go generate; fi)

tf2pulumi_coverage_report::
	make generate_tf2pulumi_coverage_input
	(cd tests/coverage-report/test && go test -v -tags=coverage -timeout 20m -run TestTemplateCoverage)

install_plugins::
	[ -x $(shell which pulumi) ] || curl -fsSL https://get.pulumi.com | sh
	pulumi plugin install resource aws 2.0.0
	pulumi plugin install resource azure 2.0.0
	pulumi plugin install resource gcp 2.0.0
	pulumi plugin install resource terraform-template 0.16.0
	pulumi plugin install resource random 2.0.0
	# Required for coverage report
	pulumi plugin install resource aws 4.7.0
	pulumi plugin install resource azure 4.6.0
	pulumi plugin install resource gcp 5.7.0
	pulumi plugin install resource github 4.0.0
	pulumi plugin install resource random 4.2.0
	pulumi plugin install resource tls 4.0.0

dev:: build lint test_acceptance

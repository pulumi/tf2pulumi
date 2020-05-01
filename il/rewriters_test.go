package il

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/stretchr/testify/assert"

	"github.com/pulumi/tf2pulumi/internal/config"
)

func TestMarkPromptDataSources(t *testing.T) {
	runTest := func(source string, expected map[string]bool) {
		dir, err := ioutil.TempDir("", "")
		if err != nil {
			t.Fatalf("could not create temporary directory: %v", err)
		}
		defer func() {
			contract.IgnoreError(os.RemoveAll(dir))
		}()

		err = ioutil.WriteFile(path.Join(dir, "main.tf"), []byte(source), 0600)
		if err != nil {
			t.Fatalf("could not create main.tf: %v", err)
		}

		conf, err := config.LoadDir(dir)
		if err != nil {
			t.Fatalf("could not load config: %v", err)
		}

		b := newBuilder(&BuildOptions{
			AllowMissingProviders: true,
			AllowMissingVariables: true,
			AllowMissingComments:  true,
		})
		err = b.buildNodes(conf)
		assert.NoError(t, err)

		set := MarkPromptDataSources(&Graph{
			Modules:   b.modules,
			Providers: b.providers,
			Resources: b.resources,
			Outputs:   b.outputs,
			Locals:    b.locals,
			Variables: b.variables,
		})

		actual := make(map[string]bool)
		for n := range set {
			actual[n.resourceID()] = true
		}

		assert.Equal(t, expected, actual)
	}

	const singlePromptDataSource = `
data "aws_subnet_ids" "example" {
  vpc_id = "${var.vpc_id}"
}
`
	runTest(singlePromptDataSource, map[string]bool{
		"data.aws_subnet_ids.example": true,
	})

	const flowPromptDataSource = `
data "aws_subnet_ids" "example" {
  vpc_id = "${var.vpc_id}"
}

data "aws_subnet" "example" {
  count = "${length(data.aws_subnet_ids.example.ids)}"
  id    = "${data.aws_subnet_ids.example.ids[count.index]}"
}
`
	runTest(flowPromptDataSource, map[string]bool{
		"data.aws_subnet_ids.example": true,
		"data.aws_subnet.example":     true,
	})

	const singleEventualDataSource = `
data "aws_subnet_ids" "example" {
  vpc_id = "${aws_vpc.default.id}"
}
`
	runTest(singleEventualDataSource, map[string]bool{})

	const flowEventualDataSource = `
data "aws_subnet_ids" "example" {
  vpc_id = "${aws_vpc.default.id}"
}

data "aws_subnet" "example" {
  count = "${length(data.aws_subnet_ids.example.ids)}"
  id    = "${data.aws_subnet_ids.example.ids[count.index]}"
}
`
	runTest(flowEventualDataSource, map[string]bool{})
}

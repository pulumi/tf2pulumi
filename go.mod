module github.com/pulumi/tf2pulumi

go 1.16

require (
	github.com/hashicorp/hcl/v2 v2.11.1
	github.com/olekukonko/tablewriter v0.0.5
	github.com/pulumi/pulumi-terraform-bridge/v3 v3.25.2
	github.com/pulumi/pulumi/pkg/v3 v3.34.1
	github.com/pulumi/pulumi/sdk/v3 v3.34.1
	github.com/spf13/afero v1.6.0
	github.com/spf13/cobra v1.4.0
	github.com/stretchr/testify v1.7.1
	modernc.org/sqlite v1.10.7
)

replace (
	github.com/coreos/etcd => github.com/pulumi/etcd v3.3.18+incompatible
	github.com/hashicorp/terraform-plugin-sdk => github.com/pulumi/terraform-plugin-sdk v0.0.0-20200416232118-ec806f20dbeb
)

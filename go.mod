module github.com/pulumi/tf2pulumi

go 1.16

require (
	github.com/pulumi/pulumi-terraform-bridge/v2 v2.18.1
	github.com/pulumi/pulumi/pkg/v2 v2.19.0
	github.com/pulumi/pulumi/sdk/v2 v2.19.0
	github.com/spf13/cobra v1.0.0
	github.com/stretchr/testify v1.6.1
	gopkg.in/airbrake/gobrake.v2 v2.0.9 // indirect
	gopkg.in/gemnasium/logrus-airbrake-hook.v2 v2.1.2 // indirect
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.4.3+incompatible
	github.com/coreos/etcd => github.com/pulumi/etcd v3.3.18+incompatible
	github.com/hashicorp/terraform-plugin-sdk => github.com/pulumi/terraform-plugin-sdk v0.0.0-20200416232118-ec806f20dbeb
)

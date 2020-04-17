module github.com/pulumi/tf2pulumi

go 1.13

require (
	github.com/apparentlymart/go-cidr v1.0.1
	github.com/blang/semver v3.5.1+incompatible
	github.com/coreos/etcd v3.3.18+incompatible // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/hashicorp/consul v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.0.0
	github.com/hashicorp/go-getter v1.4.2-0.20200106182914-9813cbd4eb02
	github.com/hashicorp/go-multierror v1.0.0
	github.com/hashicorp/go-uuid v1.0.1
	github.com/hashicorp/go-version v1.2.0
	github.com/hashicorp/hcl v1.0.0
	github.com/hashicorp/hcl/v2 v2.3.0
	github.com/hashicorp/hil v0.0.0-20190212132231-97b3a9cdfa93
	github.com/hashicorp/terraform v0.12.24
	github.com/hashicorp/terraform-plugin-sdk v1.0.0
	github.com/hashicorp/terraform-svchost v0.0.0-20191119180714-d2e4933b9136
	github.com/hashicorp/vault v1.2.0 // indirect
	github.com/mitchellh/cli v1.0.0
	github.com/mitchellh/copystructure v1.0.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/hashstructure v1.0.0
	github.com/mitchellh/mapstructure v1.1.2
	github.com/mitchellh/reflectwalk v1.0.1
	github.com/pkg/errors v0.9.1
	github.com/pulumi/pulumi-terraform-bridge v1.8.1-0.20200304180844-d15f6454d830
	github.com/pulumi/pulumi-terraform-bridge/v2 v2.0.0
	github.com/pulumi/pulumi/pkg/v2 v2.0.1-0.20200416234434-398878de31e4
	github.com/pulumi/pulumi/sdk/v2 v2.0.0
	github.com/spf13/cobra v0.0.6
	github.com/stretchr/testify v1.5.1
	github.com/terraform-providers/terraform-provider-archive v1.3.0
	github.com/terraform-providers/terraform-provider-http v1.1.1
	github.com/ugorji/go v1.1.7 // indirect
	github.com/zclconf/go-cty v1.3.1
	golang.org/x/crypto v0.0.0-20200317142112-1b76d66859c6
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.4.3+incompatible
	github.com/coreos/etcd => github.com/pulumi/etcd v3.3.18+incompatible
	github.com/hashicorp/terraform-plugin-sdk => github.com/pulumi/terraform-plugin-sdk v0.0.0-20200416232118-ec806f20dbeb
)

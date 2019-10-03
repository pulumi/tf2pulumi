module github.com/pulumi/tf2pulumi

go 1.12

require (
	github.com/Azure/go-autorest/autorest v0.9.1 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/apparentlymart/go-cidr v1.0.1
	github.com/blang/semver v3.5.1+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/hashicorp/consul v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.0.0
	github.com/hashicorp/go-getter v1.4.0
	github.com/hashicorp/go-multierror v1.0.0
	github.com/hashicorp/go-uuid v1.0.1
	github.com/hashicorp/go-version v1.2.0
	github.com/hashicorp/hcl v1.0.0
	github.com/hashicorp/hcl2 v0.0.0-20191002203319-fb75b3253c80
	github.com/hashicorp/hil v0.0.0-20190212132231-97b3a9cdfa93
	github.com/hashicorp/terraform v0.12.9
	github.com/hashicorp/terraform-plugin-sdk v1.0.0
	github.com/hashicorp/vault v1.2.0 // indirect
	github.com/kr/pty v1.1.3 // indirect
	github.com/mitchellh/cli v1.0.0
	github.com/mitchellh/copystructure v1.0.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/hashstructure v1.0.0
	github.com/mitchellh/mapstructure v1.1.2
	github.com/mitchellh/reflectwalk v1.0.1
	github.com/pkg/errors v0.8.1
	github.com/pulumi/pulumi v1.2.0
	github.com/pulumi/pulumi-terraform-bridge v1.0.0
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	github.com/terraform-providers/terraform-provider-archive v1.3.0
	github.com/terraform-providers/terraform-provider-http v1.1.1
	github.com/ugorji/go v1.1.4 // indirect
	github.com/zclconf/go-cty v1.1.0
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.4.3+incompatible
	github.com/hashicorp/terraform-plugin-sdk => github.com/pulumi/terraform-plugin-sdk v0.0.0-20191008233500-a54d0e1d3c67
	github.com/ugorji/go v1.1.4 => github.com/ugorji/go/codec v0.0.0-20181012064053-8333dd449516
	github.com/ugorji/go/codec => github.com/ugorji/go/codec v0.0.0-20181012064053-8333dd449516
)

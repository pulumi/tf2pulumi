module github.com/pulumi/tf2pulumi

go 1.12

replace (
	github.com/Nvveen/Gotty => github.com/ijc25/Gotty v0.0.0-20170406111628-a8b993ba6abd
	github.com/census-instrumentation/opencensus-proto v0.1.0 => github.com/census-instrumentation/opencensus-proto v0.1.0-0.20181214143942-ba49f56771b8
	github.com/golang/glog => github.com/pulumi/glog v0.0.0-20180820174630-7eaa6ffb71e4
	github.com/ugorji/go v1.1.4 => github.com/ugorji/go/codec v0.0.0-20181012064053-8333dd449516
	go.opencensus.io v0.20.0 => go.opencensus.io v0.18.1-0.20181204023538-aab39bd6a98b
)

require (
	git.apache.org/thrift.git v0.12.0 // indirect
	github.com/chzyer/logex v1.1.11-0.20160617073814-96a4d311aa9b // indirect
	github.com/gopherjs/gopherjs v0.0.0-20181103185306-d547d1d9531e // indirect
	github.com/hashicorp/hcl v1.0.0
	github.com/hashicorp/hil v0.0.0-20190212132231-97b3a9cdfa93
	github.com/hashicorp/serf v0.8.2-0.20171022020050-c20a0b1b1ea9 // indirect
	github.com/hashicorp/terraform v0.12.0-alpha4.0.20190424121927-9327eedb0417
	github.com/pkg/errors v0.8.1
	github.com/pulumi/pulumi v0.17.10
	github.com/pulumi/pulumi-terraform v0.14.1-dev.0.20190513174649-25d8e7a4a111
	github.com/smartystreets/assertions v0.0.0-20190116191733-b6c0e53d7304 // indirect
	github.com/spf13/cobra v0.0.3
	github.com/stretchr/testify v1.3.0
	github.com/terraform-providers/terraform-provider-archive v1.2.2
	github.com/terraform-providers/terraform-provider-http v1.1.1
	github.com/ugorji/go v1.1.4 // indirect
)

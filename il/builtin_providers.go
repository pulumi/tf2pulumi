package il

import (
	"github.com/terraform-providers/terraform-provider-archive/archive"
	"github.com/terraform-providers/terraform-provider-http/http"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
)

// builtinProviderInfo provides a static map from provider name to propvider information for the small set of providers
// that should be implemented by the target language (rather than as Pulumi providers). Currently this includes the
// archive and http providers. Resources from the former provider are translated as Pulumi assets; resources/data
// sources from the latter should be translated as calls to the target langauge's appropriate HTTP client libraries.
var builtinProviderInfo = map[string]*tfbridge.ProviderInfo{
	"archive": {
		P:      archive.Provider().(*schema.Provider),
		Config: map[string]*tfbridge.SchemaInfo{},
		DataSources: map[string]*tfbridge.DataSourceInfo{
			"archive_file": {Tok: "archive:archive:archiveFile"},
		},
		Resources: map[string]*tfbridge.ResourceInfo{
			"archive_file": {Tok: "archive:archive/archiveFile:ArchiveFile"},
		},
	},
	"http": {
		P:      http.Provider().(*schema.Provider),
		Config: map[string]*tfbridge.SchemaInfo{},
		DataSources: map[string]*tfbridge.DataSourceInfo{
			"http": {Tok: "http:http:http"},
		},
		Resources: map[string]*tfbridge.ResourceInfo{},
	},
}

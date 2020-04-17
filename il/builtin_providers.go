// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package il

import (
	"github.com/terraform-providers/terraform-provider-archive/archive"
	"github.com/terraform-providers/terraform-provider-http/http"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
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

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
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
	"github.com/pulumi/pulumi/sdk/v2/go/common/workspace"
)

// ProviderInfoSource abstracts the ability to fetch tfbridge information for a Terraform provider. This is abstracted
// primarily for testing purposes.
type ProviderInfoSource interface {
	// GetProviderInfo returns the tfbridge information for the indicated Terraform provider.
	GetProviderInfo(tfProviderName string) (*tfbridge.ProviderInfo, error)
}

// CachingProviderInfoSource wraps a ProviderInfoSource in a cache for faster access.
type CachingProviderInfoSource struct {
	source  ProviderInfoSource
	entries map[string]*tfbridge.ProviderInfo
}

// GetProviderInfo returns the tfbridge information for the indicated Terraform provider as well as the name of the
// corresponding Pulumi resource provider.
func (cache *CachingProviderInfoSource) GetProviderInfo(tfProviderName string) (*tfbridge.ProviderInfo, error) {
	info, ok := cache.entries[tfProviderName]
	if !ok {
		i, err := cache.source.GetProviderInfo(tfProviderName)
		if err != nil {
			return nil, err
		}
		cache.entries[tfProviderName], info = i, i
	}
	return info, nil
}

// NewCachingProviderInfoSource creates a new CachingProviderInfoSource that wraps the given ProviderInfoSource.
func NewCachingProviderInfoSource(source ProviderInfoSource) *CachingProviderInfoSource {
	return &CachingProviderInfoSource{
		source:  source,
		entries: map[string]*tfbridge.ProviderInfo{},
	}
}

type pluginProviderInfoSource struct{}

// PluginProviderInfoSource is the ProviderInfoSource that retrieves tfbridge information by loading and interrogating
// the Pulumi resource provider that corresponds to a Terraform provider.
var PluginProviderInfoSource = ProviderInfoSource(pluginProviderInfoSource{})

var pluginNames = map[string]string{
	"azurerm":  "azure",
	"bigip":    "f5bigip",
	"google":   "gcp",
	"template": "terraform-template",
}

// GetProviderInfo returns the tfbridge information for the indicated Terraform provider as well as the name of the
// corresponding Pulumi resource provider.
func (pluginProviderInfoSource) GetProviderInfo(tfProviderName string) (*tfbridge.ProviderInfo, error) {
	pluginName, hasPluginName := pluginNames[tfProviderName]
	if !hasPluginName {
		pluginName = tfProviderName
	}

	_, path, err := workspace.GetPluginPath(workspace.ResourcePlugin, pluginName, nil)
	if err != nil {
		return nil, err
	} else if path == "" {
		message := fmt.Sprintf("could not find plugin %s for provider %s", pluginName, tfProviderName)
		latest := getLatestPluginVersion(pluginName)
		if latest != "" {
			message += fmt.Sprintf("; try running 'pulumi plugin install resource %s %s'", pluginName, latest)
		}
		return nil, errors.New(message)
	}

	// Run the plugin and decode its provider config.
	// nolint: gas
	cmd := exec.Command(path, "-get-provider-info")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load plugin %s for provider %s", pluginName, tfProviderName)
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.Wrapf(err, "failed to load plugin %s for provider %s", pluginName, tfProviderName)
	}

	var info *tfbridge.MarshallableProviderInfo
	err = json.NewDecoder(out).Decode(&info)

	if cErr := cmd.Wait(); cErr != nil {
		return nil, errors.Wrapf(err, "failed to run plugin %s for provider %s", pluginName, tfProviderName)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "could not decode schema information for provider %s", tfProviderName)
	}

	return info.Unmarshal(), nil
}

// getLatestPluginVersion returns the version number for the latest released version of the indicated plugin by
// querying the value of the `latest` tag in the plugin's corresponding NPM package.
func getLatestPluginVersion(pluginName string) string {
	resp, err := http.Get("https://registry.npmjs.org/@pulumi/" + pluginName)
	if err != nil {
		return ""
	}
	defer contract.IgnoreClose(resp.Body)

	// The structure of the response to the above call is documented here:
	// - https://github.com/npm/registry/blob/master/docs/responses/package-metadata.md
	var packument struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&packument); err != nil {
		return ""
	}
	return packument.DistTags["latest"]
}

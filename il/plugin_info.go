package il

import (
	"encoding/json"
	"os/exec"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/workspace"
)

// ProviderInfoSource abstracts the ability to fetch tfbridge information for a Terraform provider. This is abstracted
// primarily for testing purposes.
type ProviderInfoSource interface {
	// GetProviderInfo returns the tfbridge information for the indicated Terraform provider as well as the name of the
	// corresponding Pulumi resource provider.
	GetProviderInfo(tfProviderName string) (*tfbridge.ProviderInfo, string, error)
}

type pluginProviderInfoSource struct{}

// PluginProviderInfoSource is the ProviderInfoSource that retrieves tfbridge information by loading and interrogating
// the Pulumi resource provider that corresponds to a Terraform provider.
var PluginProviderInfoSource = ProviderInfoSource(pluginProviderInfoSource{})

var pluginNames = map[string]string{
	"azurerm":  "azure",
	"google":   "gcp",
	"template": "terraform-template",
}

// GetProviderInfo returns the tfbridge information for the indicated Terraform provider as well as the name of the
// corresponding Pulumi resource provider.
func (pluginProviderInfoSource) GetProviderInfo(tfProviderName string) (*tfbridge.ProviderInfo, string, error) {
	pluginName, hasPluginName := pluginNames[tfProviderName]
	if !hasPluginName {
		pluginName = tfProviderName
	}

	_, path, err := workspace.GetPluginPath(workspace.ResourcePlugin, pluginName, nil)
	if err != nil {
		return nil, "", err
	} else if path == "" {
		return nil, "", errors.Errorf("could not find plugin %s for provider %s", pluginName, tfProviderName)
	}

	// Run the plugin and decode its provider config.
	cmd := exec.Command(path, "-get-provider-info")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, "", errors.Wrapf(err, "failed to load plugin %s for provider %s", pluginName, tfProviderName)
	}

	var info *tfbridge.MarshallableProviderInfo
	err = json.NewDecoder(out).Decode(&info)

	if cErr := cmd.Wait(); cErr != nil {
		return nil, "", errors.Wrapf(err, "failed to run plugin %s for provider %s", pluginName, tfProviderName)
	}
	if err != nil {
		return nil, "", errors.Wrapf(err, "could not decode schema information for provider %s", tfProviderName)
	}

	return info.Unmarshal(), pluginName, nil
}

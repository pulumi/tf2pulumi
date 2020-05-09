package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"

	"github.com/pulumi/pulumi/sdk/go/pulumi"
	"github.com/pulumi/pulumi/sdk/go/pulumi/config"
)

// AddImportTransformation by reading from Terraform State and add `Import` transformation for each _found_ resource
func AddImportTransformation(ctx *pulumi.Context) error {

	config := config.New(ctx, "")

	importFromStateFile := config.Get("importFromStateFile")
	if importFromStateFile == "" {
		return nil
	}

	terraformState, err := ioutil.ReadFile(importFromStateFile)
	if err != nil {
		return err
	}

	err = checkTerraformStateVersion(terraformState)
	if err != nil {
		return err
	}

	ctx.Log.Info(fmt.Sprintf("Importing terraform state from [%s]...", importFromStateFile), nil)

	var terraformResources stateV4
	err = json.Unmarshal(terraformState, &terraformResources)

	// make map of pulumi "type::name" to "id" - e.g. "aws:ec2/vpc:Vpc::main" => "vpc-abc123"
	pulumiResourceMapping := make(map[string]string)
	for _, terraformResource := range terraformResources.Resources {

		// each resource has an `Instances` array regardless of `count`,
		// we assume the order of these resources is stable and predictable
		for terraformResourceIndex, terraformResourceInstance := range terraformResource.Instances {

			// add a suffix to the resource name if resource is of list (via count) or map
			// e.g. `main`
			terraformResourceIndexSuffix := ""
			if terraformResource.EachMode == "list" {
				// e.g. `main-0`
				terraformResourceIndexSuffix = fmt.Sprintf("-%v", terraformResourceIndex)
			} else if terraformResource.EachMode == "map" {
				// e.g. `main-1.1.1.1/32`
				terraformResourceIndexSuffix = fmt.Sprintf("-%v", terraformResourceInstance.IndexKey)
			}

			var resourceAttributes map[string]interface{}
			json.Unmarshal(terraformResourceInstance.AttributesRaw, &resourceAttributes)
			if err != nil {
				return err
			}

			pulumiType := typeMapping[terraformResource.Type]
			if pulumiType == "" {
				// TODO return error if type mapping not found?
				ctx.Log.Warn(fmt.Sprintf("No type mapping for [%s]. Unable to import.", terraformResource.Type), nil)
				continue
			}

			// e.g. "vpc-abc123"
			resourceID := resourceAttributes["id"].(string)

			// override resourceID for "special" resources
			specialTypeFunc := specialMapping[terraformResource.Type]
			if specialTypeFunc != nil {
				ctx.Log.Debug(fmt.Sprintf("Using special mapping for [%s]", terraformResource.Type), nil)
				resourceID = specialTypeFunc(resourceAttributes)
			}

			// e.g. "aws:ec2/vpc:Vpc::main" or "aws:ec2/vpc:Vpc::main-0"
			pulumiResourceTypeAndName := fmt.Sprintf("%s::%s%s", pulumiType, terraformResource.Name, terraformResourceIndexSuffix)

			// e.g. "aws:ec2/vpc:Vpc::main" => "vpc-abc123"
			pulumiResourceMapping[pulumiResourceTypeAndName] = resourceID
			ctx.Log.Debug(fmt.Sprintf("Mapped [%s] => [%s]", pulumiResourceTypeAndName, resourceID), nil)

			// Map by 'Name' tag too
			tags := resourceAttributes["tags"]
			if tags != nil {
				// Only handle maps for now
				tagsType := reflect.ValueOf(tags)
				if tagsType.Kind() == reflect.Map {
					ctx.Log.Debug(fmt.Sprintf("parsing name tags %s", pulumiResourceTypeAndName), nil)
					nameTag := tags.(map[string]interface{})["Name"]
					if nameTag != "" {
						pulumiResourceTypeAndName := fmt.Sprintf("%s::%s%s", pulumiType, nameTag, terraformResourceIndexSuffix)
						pulumiResourceMapping[pulumiResourceTypeAndName] = resourceID
						ctx.Log.Debug(fmt.Sprintf("Mapped [%s] => [%s] by Name tag", pulumiResourceTypeAndName, resourceID), nil)
					}
				} else {
					ctx.Log.Warn(fmt.Sprintf("`tags` is not of type map. Skipping Name tag parsing for [%s]...", pulumiResourceTypeAndName), nil)
				}
			}
		}
	}

	transformation := func(args *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
		return &pulumi.ResourceTransformationResult{
			Props: args.Props,
			Opts:  append(args.Opts, pulumi.Import(pulumi.ID(lookupID(ctx, pulumiResourceMapping, args)))),
		}
	}

	ctx.RegisterStackTransformation(transformation)
	return nil
}

// lookupID by mapping `aws:ec2/vpc:Vpc` to `aws_vpc` and then finding `aws_vpc.${name}` in `terraformResourceMapping`
func lookupID(ctx *pulumi.Context, pulumiResourceMapping map[string]string, args *pulumi.ResourceTransformationArgs) string {

	// e.g. "aws:ec2/vpc:Vpc::main"
	pulumiResourceTypeAndName := fmt.Sprintf("%s::%s", args.Type, args.Name)

	// find resource id - e.g. "aws:ec2/vpc:Vpc::main" => "vpc-abc123"
	foundResource := pulumiResourceMapping[pulumiResourceTypeAndName]
	if foundResource == "" {
		// TODO return error if resource mapping not found?
		ctx.Log.Warn(fmt.Sprintf("Unable to import [%s]. Either not found in Terraform state or no type mapping.", pulumiResourceTypeAndName), nil)
		return ""
	}

	return foundResource
}

// checkTerraformStateVersion and return an error if version is not `4`
func checkTerraformStateVersion(terraformState []byte) error {
	type VersionSniff struct {
		Version *uint64 `json:"version"`
	}
	var sniff VersionSniff
	err := json.Unmarshal(terraformState, &sniff)
	if err != nil {
		return err
	}

	if *sniff.Version != 4 {
		return errors.New("Only version 4 state files are supported")
	}
	return nil
}

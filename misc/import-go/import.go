package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/pulumi/pulumi/sdk/go/pulumi"
)

// AddImportTransformation by reading from Terraform State and add `Import` transformation for each _found_ resource
func AddImportTransformation(ctx *pulumi.Context, importFromStateFile string) error {

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
		// each resource has an `Instances` array regardless of `count`
		resourceInstanceLength := len(terraformResource.Instances)
		for _, resourceInstance := range terraformResource.Instances {
			terraformResourceIndexKey := 0
			terraformResourceIndexSuffix := ""

			// if resource was created with `count` in terraform, use the `index_key` instead of `0`
			if resourceInstanceLength > 1 {
				terraformResourceIndexKey = int(resourceInstance.IndexKey.(float64)) // By default, Go treats numeric values in JSON as float64
				terraformResourceIndexSuffix = fmt.Sprintf("-%v", resourceInstance.IndexKey)
			}

			var resourceAttributes map[string]interface{}
			json.Unmarshal(terraformResource.Instances[terraformResourceIndexKey].AttributesRaw, &resourceAttributes)

			pulumiType := typeMapping[terraformResource.Type]
			if pulumiType == "" {
				// TODO return error if type mapping not found?
				ctx.Log.Warn(fmt.Sprintf("Mapping for [%s] not found. Skipping import of [%s]...", terraformResource.Type, terraformResource.Name), nil)
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
		ctx.Log.Warn(fmt.Sprintf("Resource mapping for [%s] not found. Skipping import...", pulumiResourceTypeAndName), nil)
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

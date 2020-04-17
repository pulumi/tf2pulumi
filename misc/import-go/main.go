package main

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/go/pulumi"
	"github.com/pulumi/pulumi/sdk/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		config := config.New(ctx, "")

		/**
		 * Add this directive to your Pulumi application to
		 * run the import.
		 */
		importFromStateFile := config.Get("importFromStateFile")
		if importFromStateFile != "" {
			err := AddImportTransformation(ctx, importFromStateFile)
			if err != nil {
				return err
			}
		}

		/**
		 * These resources are here as examples for doing an import.
		 * This is where you would put your own resources.
		 */
		vpc, err := ec2.NewVpc(ctx, "main", &ec2.VpcArgs{
			CidrBlock: pulumi.String("10.0.0.0/16"),
		})
		if err != nil {
			return err
		}

		igw, err := ec2.NewInternetGateway(ctx, "main", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
		})
		if err != nil {
			return err
		}

		publicRouteTable, err := ec2.NewRouteTable(ctx, "main", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: &ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
		})
		if err != nil {
			return err
		}

		publicSubnetCidrBlocks := []string{"10.0.0.0/24", "10.0.1.0/24"}
		for i, cidrBlock := range publicSubnetCidrBlocks {
			publicSubnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("public-%v", i), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(cidrBlock),
				MapPublicIpOnLaunch: pulumi.Bool(true),
			})
			if err != nil {
				return err
			}

			_, err = ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("public-%v", i), &ec2.RouteTableAssociationArgs{
				RouteTableId: publicRouteTable.ID(),
				SubnetId:     publicSubnet.ID(),
			})
			if err != nil {
				return err
			}

		}

		privateSubnetCidrBlocks := []string{"10.0.2.0/24", "10.0.3.0/24"}
		for i, cidrBlock := range privateSubnetCidrBlocks {
			_, err = ec2.NewSubnet(ctx, fmt.Sprintf("private-%v", i), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(cidrBlock),
				MapPublicIpOnLaunch: pulumi.Bool(false),
			})
			if err != nil {
				return err
			}
		}

		_, err = s3.NewBucket(ctx, "main", &s3.BucketArgs{})
		if err != nil {
			return err
		}

		ctx.Export("vpcId", vpc.ID())
		return nil
	})
}

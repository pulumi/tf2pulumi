package main

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/go/aws/iot"
	"github.com/pulumi/pulumi-aws/sdk/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		/**
		 * Add this directive to your Pulumi application to
		 * run the import.
		 */
		err := AddImportTransformation(ctx)
		if err != nil {
			return err
		}

		_, err = s3.NewBucket(ctx, "main", &s3.BucketArgs{
			Bucket: pulumi.String("import-apr15-1841"),
		})

		vpc, err := ec2.NewVpc(ctx, "main", &ec2.VpcArgs{
			CidrBlock: pulumi.String("10.0.0.0/16"),
			Tags: pulumi.Map{
				"project_name": pulumi.String("original-tf"),
				"stack_name":   pulumi.String(ctx.Stack()),
				"Name":         pulumi.String("main"),
			},
		})
		if err != nil {
			return err
		}

		igw, err := ec2.NewInternetGateway(ctx, "main", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.Map{
				"project_name": pulumi.String("original-tf"),
				"stack_name":   pulumi.String(ctx.Stack()),
				"Name":         pulumi.String("public"),
			},
		})
		if err != nil {
			return err
		}

		publicRouteTable, err := ec2.NewRouteTable(ctx, "main", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
		})
		if err != nil {
			return err
		}

		_, err = ec2.NewRoute(ctx, "public", &ec2.RouteArgs{
			RouteTableId:         publicRouteTable.ID(),
			DestinationCidrBlock: pulumi.String("0.0.0.0/0"),
			GatewayId:            igw.ID(),
		})
		if err != nil {
			return err
		}

		routedCidrBlocks := []string{"1.1.1.1/32", "2.2.2.2/32"}
		for _, cidrBlock := range routedCidrBlocks {
			_, err = ec2.NewRoute(ctx, fmt.Sprintf("%s-%s", "routed", cidrBlock), &ec2.RouteArgs{
				RouteTableId:         publicRouteTable.ID(),
				DestinationCidrBlock: pulumi.String(cidrBlock),
				GatewayId:            igw.ID(),
			})
			if err != nil {
				return err
			}
		}

		publicSubnetCidrBlocks := []string{"10.0.0.0/24", "10.0.1.0/24"}
		for i, cidrBlock := range publicSubnetCidrBlocks {
			publicSubnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("public-%v", i), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(cidrBlock),
				MapPublicIpOnLaunch: pulumi.Bool(true),
				Tags: pulumi.Map{
					"project_name": pulumi.String("original-tf"),
					"stack_name":   pulumi.String(ctx.Stack()),
					"Name":         pulumi.String("public"),
				},
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
				Tags: pulumi.Map{
					"project_name": pulumi.String("original-tf"),
					"stack_name":   pulumi.String(ctx.Stack()),
					"Name":         pulumi.String("private"),
				},
			})
			if err != nil {
				return err
			}
		}

		_, err = s3.NewBucket(ctx, "not-found", &s3.BucketArgs{})
		if err != nil {
			return err
		}

		_, err = iot.NewThing(ctx, "no-type-mapping", &iot.ThingArgs{})
		if err != nil {
			return err
		}

		ctx.Export("vpcId", vpc.ID())
		return nil
	})
}

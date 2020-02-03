import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const config = new pulumi.Config();
// Accept the AWS region as input.
const awsRegion = config.get("awsRegion") || "us-west-2";

// Create a VPC.
//
// Note that the VPC has been tagged appropriately.
const defaultVpc = new aws.ec2.Vpc("default", {
    cidrBlock: "10.0.0.0/16", // Just one CIDR block
    enableDnsHostnames: true, // Definitely want DNS hostnames.
    // The tag collection for this VPC.
    tags: {
        // Ensure that we tag this VPC with a Name.
        Name: "test",
    },
});
// Use some data sources.
const defaultSubnetIds = defaultVpc.id.apply(id => aws.ec2.getSubnetIds({
    vpcId: id,
}, { async: true }));
const defaultAvailabilityZones = aws.getAvailabilityZones();
const defaultAvailabilityZone: aws.GetAvailabilityZoneResult[] = [];
for (let i = 0; i < defaultAvailabilityZones.ids.length; i++) {
    defaultAvailabilityZone.push(aws.getAvailabilityZone({
        zoneId: defaultAvailabilityZones.zoneIds[i],
    }));
}
// The VPC details
const vpc = [{
    // The ID
    id: defaultVpc.id,
}];
// The region, again
const region = awsRegion; // why not
// Create a security group.
//
// This group should allow SSH and HTTP access.
const defaultSecurityGroup = new aws.ec2.SecurityGroup("default", {
    // outbound internet access
    egress: [{
        cidrBlocks: ["0.0.0.0/0"],
        fromPort: 0,
        protocol: "-1", // All
        toPort: 0,
    }],
    ingress: [
        // SSH access from anywhere
        {
            // "0.0.0.0/0" is anywhere
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 22,
            protocol: "tcp",
            toPort: 22,
        },
        // HTTP access from anywhere
        {
            cidrBlocks: ["0.0.0.0/0"],
            fromPort: 80,
            protocol: "tcp", // HTTP is TCP-only
            toPort: 80,
        },
    ],
    tags: {
        Vpc: pulumi.interpolate`VPC ${awsRegion}:${defaultVpc.id}`,
    },
    vpcId: vpc["id"],
});

// Output the SG name.
//
// We pull the name from the default SG.
// Take the value from the default SG.
export const securityGroupName = defaultSecurityGroup.name; // Neat!

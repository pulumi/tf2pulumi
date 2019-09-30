import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const config = new pulumi.Config();
const createSg = config.getBoolean("createSg") || false;
// Accept the AWS region as input.
const awsRegion = config.get("awsRegion") || "us-west-2";

const inUsEast1 = (awsRegion === "us-east-1");
// Optionally create a security group and attach some rules.
let defaultSecurityGroup: aws.ec2.SecurityGroup | undefined;
if (createSg) {
    defaultSecurityGroup = new aws.ec2.SecurityGroup("default", {
        description: "Default security group",
    });
}
// SSH access from anywhere
let ingress: aws.ec2.SecurityGroupRule | undefined;
if (createSg) {
    ingress = new aws.ec2.SecurityGroupRule("ingress", {
        cidrBlocks: ["0.0.0.0/0"],
        fromPort: 22,
        protocol: "tcp",
        securityGroupId: defaultSecurityGroup!.id,
        toPort: 22,
        type: "ingress",
    });
}
// outbound internet access
let egress: aws.ec2.SecurityGroupRule | undefined;
if (createSg) {
    egress = new aws.ec2.SecurityGroupRule("egress", {
        cidrBlocks: ["0.0.0.0/0"],
        fromPort: 0,
        protocol: "-1",
        securityGroupId: defaultSecurityGroup!.id,
        toPort: 0,
        type: "ingress",
    });
}
// If we are in us-east-1, create an ec2 instance
let web: aws.ec2.Instance | undefined;
if (inUsEast1) {
    web = new aws.ec2.Instance("web", {
        ami: "some-ami",
        instanceType: "t2.micro",
        tags: {
            Name: "HelloWorld",
        },
    });
}
// If we are in us-east-2, create a different ec2 instance
const createWeb2 = ((awsRegion === "us-east-2") ? 1 : 0);
let web2: aws.ec2.Instance | undefined;
if (!!(createWeb2)) {
    web2 = new aws.ec2.Instance("web2", {
        ami: "some-other-ami",
        instanceType: "t2.micro",
        tags: {
            Name: `instance-${(createWeb2 % 2)}`,
        },
    });
}

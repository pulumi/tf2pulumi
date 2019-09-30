import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const config = new pulumi.Config();
// Originally defined at variables.tf:1
const vpcId = config.require("vpcId");
// Originally defined at variables.tf:3
const availabilityZone = config.require("availabilityZone");
// Originally defined at variables.tf:13
const regionNumbers = config.get("regionNumbers") || {
    "eu-west-1": 4,
    "us-east-1": 1,
    "us-west-1": 2,
    "us-west-2": 3,
};
// Originally defined at variables.tf:22
const azNumbers = config.get("azNumbers") || {
    a: 1,
    b: 2,
    c: 3,
    d: 4,
    e: 5,
    f: 6,
    g: 7,
    h: 8,
    i: 9,
    j: 10,
    k: 11,
    l: 12,
    m: 13,
    n: 14,
};

// Originally defined at variables.tf:5
const targetAvailabilityZone = aws.getAvailabilityZone({
    name: availabilityZone,
});
// Originally defined at variables.tf:9
const targetVpc = aws.ec2.getVpc({
    id: vpcId,
});
// Originally defined at subnet.tf:5
const mainSubnet = new aws.ec2.Subnet("main", {
    availabilityZone: availabilityZone,
    cidrBlock: (() => {
        throw "tf2pulumi error: NYI: call to cidrsubnet";
        return (() => { throw "NYI: call to cidrsubnet"; })();
    })(),
    vpcId: vpcId,
});
// Originally defined at subnet.tf:11
const mainRouteTable = new aws.ec2.RouteTable("main", {
    vpcId: vpcId,
});
// Originally defined at subnet.tf:1
const mainRouteTableAssociation = new aws.ec2.RouteTableAssociation("main", {
    routeTableId: mainRouteTable.id,
    subnetId: mainSubnet.id,
});
// Originally defined at security_group.tf:6
const az = new aws.ec2.SecurityGroup("az", {
    // name        = "az-${data.aws_availability_zone.target.name}"
    description: `Open access within the AZ ${targetAvailabilityZone.name!}`,
    ingress: [{
        cidrBlocks: [mainSubnet.cidrBlock],
        fromPort: 0,
        protocol: "-1",
        toPort: 0,
    }],
    vpcId: vpcId,
});

// Originally defined at outputs.tf:1
export const subnetId = mainSubnet.id;

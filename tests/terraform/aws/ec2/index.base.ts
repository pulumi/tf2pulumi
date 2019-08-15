import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const ubuntu = aws.getAmi({
    filters: [
        {
            name: "name",
            values: ["ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server-*"],
        },
        {
            name: "virtualization-type",
            values: ["hvm"],
        },
    ],
    mostRecent: true,
    owners: ["099720109477"],
});
const web = new aws.ec2.Instance("web", {
    ami: ubuntu.id,
    instanceType: "t2.micro",
    tags: {
        Name: "HelloWorld",
    },
});

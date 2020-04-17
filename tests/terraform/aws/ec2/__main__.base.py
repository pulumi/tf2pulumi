import pulumi
import json
import pulumi_aws as aws

ubuntu = aws.ec2.Ami("ubuntu",
    filter=[
        {
            "name": "name",
            "values": ["ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server-*"],
        },
        {
            "name": "virtualization-type",
            "values": ["hvm"],
        },
    ],
    most_recent=True,
    owners=["099720109477"])
web = aws.ec2.Instance("web",
    ami=ubuntu.id,
    instance_type="t2.micro",
    tags={
        "Name": "HelloWorld",
    })

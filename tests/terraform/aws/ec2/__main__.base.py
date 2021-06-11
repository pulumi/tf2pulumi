import pulumi
import pulumi_aws as aws

ubuntu = aws.get_ami(filters=[
        aws.GetAmiFilterArgs(
            name="name",
            values=["ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server-*"],
        ),
        aws.ec2.GetAmiFilterArgs(
            name="virtualization-type",
            values=["hvm"],
        ),
    ],
    most_recent=True,
    owners=["099720109477"])
web = aws.Instance("web",
    ami=ubuntu.id,
    instance_type="t2.micro",
    tags={
        "Name": "HelloWorld",
    })

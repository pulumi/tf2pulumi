import pulumi
import pulumi_aws as aws

ubuntu = pulumi.Output.from_input(aws.get_ami(filter=[{"name": "name", "values": ["ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server-*"]}, {"name": "virtualization-type", "values": ["hvm"]}], most_recent=True, owners=["099720109477"]))
web = aws.ec2.Instance("web", ami=ubuntu.id, instance_type="t2.micro", tags={"Name": "HelloWorld"})

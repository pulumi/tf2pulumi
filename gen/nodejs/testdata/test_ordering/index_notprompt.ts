import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const config = new pulumi.Config();
// Accept the AWS region as input.
const awsRegion = config.get("awsRegion") || "us-west-2";

// Create a provider for account data.
const accountData = new aws.Provider("account_data", {
    region: awsRegion,
});
// Get the caller's identity.
const accountDataCallerIdentity = pulumi.output(aws.getCallerIdentity({ provider: accountData, async: true }));

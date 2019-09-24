import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Originally defined at main.tf:12
const pool = new aws.cognito.UserPool("pool", {});
// Originally defined at main.tf:16
const client = new aws.cognito.UserPoolClient("client", {});
// Originally defined at main.tf:20
const domain = new aws.cognito.UserPoolDomain("domain", {});
// Originally defined at main.tf:4
const frontEndLoadBalancer = new aws.lb.LoadBalancer("front_end", {});
// Originally defined at main.tf:8
const frontEndTargetGroup = new aws.lb.TargetGroup("front_end", {});
// Originally defined at main.tf:24
const frontEndListener = new aws.lb.Listener("front_end", {
    defaultActions: [
        {
            authenticateCognito: {
                userPoolArn: pool.arn,
                userPoolClientId: client.id,
                userPoolDomain: domain.domain,
            },
            type: "authenticate-cognito",
        },
        {
            targetGroupArn: frontEndTargetGroup.arn,
            type: "forward",
        },
    ],
    loadBalancerArn: frontEndLoadBalancer.arn,
    port: 80,
    protocol: "HTTP",
});

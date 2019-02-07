import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const pool = new aws.cognito.UserPool("pool", {});
const client = new aws.cognito.UserPoolClient("client", {});
const domain = new aws.cognito.UserPoolDomain("domain", {});
const frontEndLoadBalancer = new aws.elasticloadbalancingv2.LoadBalancer("front_end", {});
const frontEndTargetGroup = new aws.elasticloadbalancingv2.TargetGroup("front_end", {});
const frontEndListener = new aws.elasticloadbalancingv2.Listener("front_end", {
    defaultAction: pulumi.all([pool.arn, client.id, domain.domain, frontEndTargetGroup.arn]).apply(([poolArn, id, domain, frontEndTargetGroupArn]) => (() => {
        throw "tf2pulumi error: aws_lb_listener.front_end.default_action: expected at most one item in list, got 2";
        return [
            {
                authenticateCognito: [{
                    userPoolArn: poolArn,
                    userPoolClientId: id,
                    userPoolDomain: domain,
                }],
                type: "authenticate-cognito",
            },
            {
                targetGroupArn: frontEndTargetGroupArn,
                type: "forward",
            },
        ];
    })()),
    loadBalancerArn: frontEndLoadBalancer.arn,
    port: 80,
    protocol: "HTTP",
});

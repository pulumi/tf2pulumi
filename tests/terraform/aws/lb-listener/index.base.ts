import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const aws_cognito_user_pool_pool = new aws.cognito.UserPool("pool", {});
const aws_cognito_user_pool_client_client = new aws.cognito.UserPoolClient("client", {});
const aws_cognito_user_pool_domain_domain = new aws.cognito.UserPoolDomain("domain", {});
const aws_lb_front_end = new aws.elasticloadbalancingv2.LoadBalancer("front_end", {});
const aws_lb_target_group_front_end = new aws.elasticloadbalancingv2.TargetGroup("front_end", {});
const aws_lb_listener_front_end = new aws.elasticloadbalancingv2.Listener("front_end", {
    defaultAction: pulumi.all([aws_cognito_user_pool_pool.arn, aws_cognito_user_pool_client_client.id, aws_cognito_user_pool_domain_domain.domain, aws_lb_target_group_front_end.arn]).apply(([__arg0, __arg1, __arg2, __arg3]) => (() => {
        throw "tf2pulumi error: aws_lb_listener.front_end.default_action: expected at most one item in list, got 2";
        return [
            {
                authenticateCognito: [{
                    userPoolArn: __arg0,
                    userPoolClientId: __arg1,
                    userPoolDomain: __arg2,
                }],
                type: "authenticate-cognito",
            },
            {
                targetGroupArn: __arg3,
                type: "forward",
            },
        ];
    })()),
    loadBalancerArn: aws_lb_front_end.arn,
    port: Number.parseFloat("80"),
    protocol: "HTTP",
});

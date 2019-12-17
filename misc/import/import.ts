import * as pulumi from "@pulumi/pulumi";
import * as fs from "fs";

const config = new pulumi.Config();
const importFromStatefile = config.get("importFromStatefile");

// Note: If when using this script to import existing resources you get warnings about properties
// being different which will cause the import to fail, it is most likely the case that the value
// you are providing is somehow not the normalzied value stored by the cloud provider.  There are
// two options for addressing this:
// 1. Add the `ignoreChanges: ["name"]` resource option for any property names that are triggering
//    this to your program source code.
// 2. Modify your program to pass the normalized version of the value.

// This is currently a manual mapping of TypeScript type names to Terraform type names.  If using
// this in your own project you will get errors for any types missing from this mapping list, and
// should add the appropriate mapping lines for the resource types you need.  In most cases, you
// will only use 10-20 resource types even in large projects.  We expect to be able to auto-generate
// this mapping in the future.
const typeMapping: Record<string, string | null> = {
    "aws:elb/appCookieStickinessPolicy:AppCookieStickinessPolicy": "aws_app_cookie_stickiness_policy",
    "aws:cloudfront/distribution:Distribution": "aws_cloudfront_distribution",
    "aws:cloudwatch/metricAlarm:MetricAlarm": "aws_cloudwatch_metric_alarm",
    "aws:rds/instance:Instance": "aws_db_instance",
    "aws:rds/parameterGroup:ParameterGroup": "aws_db_parameter_group",
    "aws:rds/subnetGroup:SubnetGroup": "aws_db_subnet_group",
    "aws:elasticache/cluster:Cluster": "aws_elasticache_cluster",
    "aws:elasticache/parameterGroup:ParameterGroup": "aws_elasticache_parameter_group",
    "aws:elasticache/replicationGroup:ReplicationGroup": "aws_elasticache_replication_group",
    "aws:elasticache/subnetGroup:SubnetGroup": "aws_elasticache_subnet_group",
    "aws:elb/loadBalancer:LoadBalancer": "aws_elb",
    "aws:iam/instanceProfile:InstanceProfile": "aws_iam_instance_profile",
    "aws:iam/policy:Policy": "aws_iam_policy",
    "aws:iam/policyAttachment:PolicyAttachment": null,
    "aws:iam/role:Role": "aws_iam_role",
    "aws:iam/rolePolicy:RolePolicy": "aws_iam_role_policy",
    "aws:iam/rolePolicyAttachment:RolePolicyAttachment": null,
    "aws:ec2/instance:Instance": "aws_instance",
    "aws:kms/alias:Alias": "aws_kms_alias",
    "aws:kms/key:Key": "aws_kms_key",
    "aws:elb/sslNegotiationPolicy:SslNegotiationPolicy": "aws_lb_ssl_negotiation_policy",
    "aws:elb/listenerPolicy:ListenerPolicy": "aws_load_balancer_listener_policy",
    "aws:route53/record:Record": "aws_route53_record",
    "aws:ec2/securityGroup:SecurityGroup": "aws_security_group",
    "aws:shield/protection:Protection": "aws_shield_protection",
    "aws:ec2/vpc:Vpc": "aws_vpc",
    "aws:ec2/subnet:Subnet": "aws_subnet",
    "aws:ec2/internetGateway:InternetGateway": "aws_internet_gateway",
    "aws:ec2/routeTable:RouteTable": "aws_route_table",
    "aws:ec2/routeTableAssociation:RouteTableAssociation": "aws_route_table_association",
    "aws:ecs/cluster:Cluster": "aws_ecs_cluster",
    "aws:cloudwatch/logGroup:LogGroup": "aws_cloudwatch_log_group",
    "aws:ec2/launchConfiguration:LaunchConfiguration": "aws_launch_configuration",
    "aws:autoscaling/group:Group": "aws_autoscaling_group",
    "aws:ecs/taskDefinition:TaskDefinition": "aws_ecs_task_definition",
    "aws:alb/targetGroup:TargetGroup": "aws_alb_target_group",
    "aws:alb/loadBalancer:LoadBalancer": "aws_alb",
    "aws:alb/listener:Listener": "aws_alb_listener",
    "aws:ecs/service:Service": "aws_ecs_service",
};

// Most resources can be imported by passing their `id`, but a few need to be imported using some
// other property of the resource.  This table includes any of these exceptions.  If you get errors
// or warnings about resources not being able to be found or the format of resource ids being
// incorrect, add a mapping here that constructs the correct id format based on the property values
// in the Terraform state file.
const idMapping: Record<string, (attrs: any) => string> = {
    "aws_ecs_cluster": attrs => attrs.name,
    "aws_ecs_service": attrs => `${attrs.cluster.split("/").pop()}/${attrs.name}`,
    "aws_ecs_task_definition": attrs => attrs.arn,
    "aws_route_table_association": attrs => `${attrs["subnet_id"]}/${attrs["route_table_id"]}`,
}

// If the `importFromStatefile` config variable has been provideed, add `import` attributes to all
// resources based on the mapping in the corresponding statefile.
if (importFromStatefile) {
    // Read the `.tfstate` file specified and extract out the logical TF names and cloud resource
    // ids defined in it.
    const statefileString = fs.readFileSync(importFromStatefile).toString()
    const data = JSON.parse(statefileString);
    if (data.version !== 3) throw new Error("Only version '3' tfstate files currently supported: " + data.version);
    if (data.modules.length != 1) throw new Error("Only a single module is currently supported.");
    const mapping: Record<string, string> = {};
    for (const tfname of Object.keys(data.modules[0].resources)) {
        const tfdata = data.modules[0].resources[tfname];
        const propMapper = idMapping[tfdata.type];
        mapping[tfname] = propMapper 
            ? propMapper(tfdata.primary.attributes) 
            : tfdata.primary.id;
    }

    // Add an `import` mapping to every resource defined in this stack. 
    pulumi.runtime.registerStackTransformation(({ type, name, props, opts }) => {
        return {
            props,
            opts: pulumi.mergeOptions(opts, { 
                import: lookupId(type, name),
            }),
        };
    });

    // Looks up a physical ID to import for the given type and name
    function lookupId(t: string, name: string): string | undefined {
        const tfType = typeMapping[t];
        if (tfType === undefined) throw new Error("Unknown TF type: " + t);
        if (tfType === null) return undefined;
        // Check if the name ends with a `-1` indicating it is part of an array of values
        const res = /^(.*)-(\d)$/.exec(name);
        if (res) {
            const indexed = mapping[`${tfType}.${res[1]}.${res[2]}`]
            if (!indexed) {
                return mapping[`${tfType}.${res[1]}`]
            }
            return indexed
        } else {
            return mapping[`${tfType}.${name}`]
        }
    }
}

package main

import "fmt"

/**
 * Special mappings
 */
func idRouteTable(resourceAttributes map[string]interface{}) string {
	return fmt.Sprintf("%s_%s", resourceAttributes["route_table_id"].(string), resourceAttributes["destination_cidr_block"].(string))
}
func idRouteTableAssociation(resourceAttributes map[string]interface{}) string {
	return fmt.Sprintf("%s/%s", resourceAttributes["subnet_id"].(string), resourceAttributes["route_table_id"].(string))
}
func idRolePolicyAttachment(resourceAttributes map[string]interface{}) string {
	return fmt.Sprintf("%s/%s", resourceAttributes["role"].(string), resourceAttributes["policy_arn"].(string))
}

var specialMapping = map[string]func(resourceAttributes map[string]interface{}) string{
	// AWS
	"aws_route":                      idRouteTable,
	"aws_route_table_association":    idRouteTableAssociation,
	"aws_iam_role_policy_attachment": idRolePolicyAttachment,
}

/**
 * Static mappings
 */
var typeMapping = map[string]string{
	// AWS
	"aws_ami":                               "aws:ec2/ami:Ami",
	"aws_autoscaling_group":                 "aws:autoscaling/group:Group",
	"aws_cloudwatch_log_group":              "aws:cloudwatch/logGroup:LogGroup",
	"aws_db_subnet_group":                   "aws:rds/subnetGroup:SubnetGroup",
	"aws_default_network_acl":               "aws:ec2/defaultNetworkAcl:DefaultNetworkAcl",
	"aws_ecr_lifecycle_policy":              "aws:ecr/lifecyclePolicy:LifecyclePolicy",
	"aws_ecr_repository":                    "aws:ecr/repository:Repository",
	"aws_ecr_repository_policy":             "aws:ecr/repositoryPolicy:RepositoryPolicy",
	"aws_egress_only_internet_gateway":      "aws:ec2/egressOnlyInternetGateway:EgressOnlyInternetGateway",
	"aws_eip":                               "aws:ec2/eip:Eip",
	"aws_eks_cluster":                       "aws:eks/cluster:Cluster",
	"aws_elasticache_subnet_group":          "aws:elasticache/subnetGroup:SubnetGroup",
	"aws_elb":                               "aws:elb/loadBalancer:LoadBalancer",
	"aws_iam_instance_profile":              "aws:iam/instanceProfile:InstanceProfile",
	"aws_iam_openid_connect_provider":       "aws:iam/openIdConnectProvider:OpenIdConnectProvider",
	"aws_iam_policy":                        "aws:iam/policy:Policy",
	"aws_iam_role":                          "aws:iam/role:Role",
	"aws_iam_role_policy":                   "aws:iam/rolePolicy:RolePolicy",
	"aws_iam_role_policy_attachment":        "aws:iam/rolePolicyAttachment:RolePolicyAttachment",
	"aws_internet_gateway":                  "aws:ec2/internetGateway:InternetGateway",
	"aws_launch_configuration":              "aws:ec2/launchConfiguration:LaunchConfiguration",
	"aws_launch_template":                   "aws:ec2/launchTemplate:LaunchTemplate",
	"aws_nat_gateway":                       "aws:ec2/natGateway:NatGateway",
	"aws_network_acl":                       "aws:ec2/networkAcl:NetworkAcl",
	"aws_network_acl_rule":                  "aws:ec2/networkAclRule:NetworkAclRule",
	"aws_redshift_subnet_group":             "aws:redshift/subnetGroup:SubnetGroup",
	"aws_route":                             "aws:ec2/route:Route",
	"aws_route53_record":                    "aws:route53/record:Record",
	"aws_route53_resolver_endpoint":         "aws:route53/resolverEndpoint:ResolverEndpoint",
	"aws_route53_resolver_rule":             "aws:route53/resolverRule:ResolverRule",
	"aws_route53_resolver_rule_association": "aws:route53/resolverRuleAssociation:ResolverRuleAssociation",
	"aws_route53_zone":                      "aws:route53/zone:Zone",
	"aws_route_table":                       "aws:ec2/routeTable:RouteTable",
	"aws_route_table_association":           "aws:ec2/routeTableAssociation:RouteTableAssociation",
	"aws_s3_bucket":                         "aws:s3/bucket:Bucket",
	"aws_security_group":                    "aws:ec2/securityGroup:SecurityGroup",
	"aws_security_group_rule":               "aws:ec2/securityGroupRule:SecurityGroupRule",
	"aws_subnet":                            "aws:ec2/subnet:Subnet",
	"aws_vpc":                               "aws:ec2/vpc:Vpc",
	"aws_vpc_dhcp_options":                  "aws:ec2/vpcDhcpOptions:VpcDhcpOptions",
	"aws_vpc_dhcp_options_association":      "aws:ec2/vpcDhcpOptionsAssociation:VpcDhcpOptionsAssociation",
	"aws_vpc_endpoint":                      "aws:ec2/vpcEndpoint:VpcEndpoint",
	"aws_vpc_endpoint_service":              "aws:ec2/vpcEndpointService:VpcEndpointService",
	"aws_vpc_ipv4_cidr_block_association":   "aws:ec2/vpcIpv4CidrBlockAssociation:VpcIpv4CidrBlockAssociation",
	"aws_vpc_peering_connection":            "aws:ec2/vpcPeeringConnection:VpcPeeringConnection",
	"aws_vpn_gateway":                       "aws:ec2/vpnGateway:VpnGateway",
	"aws_vpn_gateway_attachment":            "aws:ec2/vpnGatewayAttachment:VpnGatewayAttachment",
	"aws_vpn_gateway_route_propagation":     "aws:ec2/vpnGatewayRoutePropagation:VpnGatewayRoutePropagation",
	// Random
	"random_pet":    "random:index/randomPet:RandomPet",
	"random_string": "random:index/randomString:RandomString",
}

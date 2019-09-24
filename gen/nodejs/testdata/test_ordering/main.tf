# Accept the AWS region as input.
variable "aws_region" {
	# Default to us-west-2
	default = "us-west-2"
}

# Create a provider for account data.
provider "aws" {
  region = "${var.aws_region}"
  alias  = "account_data"
}

# Get the caller's identity.
data "aws_caller_identity" "account_data" {
  provider = "aws.account_data"
}

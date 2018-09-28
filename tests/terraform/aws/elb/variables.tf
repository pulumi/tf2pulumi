variable "aws_region" {
  description = "AWS region to launch servers."
  default     = "us-east-1"
}

# Amazon Linux 2018.03
variable "aws_amis" {
  default = {
    "us-east-1" = "ami-0ff8a91507f77f867"
    "us-west-2" = "ami-a0cfeed8"
  }
}

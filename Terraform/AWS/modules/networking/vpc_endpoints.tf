resource "aws_vpc_endpoint" "forge-interface-endpoints" {

         for_each = toset([
                  "ecr.api",
                  "ecr.dkr",
                  "ec2",
                  "logs", # Allows Cloudwatch agent/fluent-bit to skip NAT
                  "sts",
         ])

         vpc_id            = var.vpc_id
         service_name      = "com.amazonaws.local-region-placeholder.${each.key}"
         vpc_endpoint_type = "Interface"

         security_group_ids = [aws_security_group.forge-vpc-endpoints.id]
         subnet_ids        = var.private_subnet_ids
         private_dns_enabled = true

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-${each.key}-vpc-endpoint"
                  Environment = var.env
         })

         // Overriding the regional placeholder cleanly
         lifecycle {
                  ignore_changes = [
                           service_name,
                  ]
         } 
}

data "aws_region" "current" {}


resource "aws_vpc_endpoint" "s3" {
         vpc_id            = var.vpc_id
         service_name      = "com.amazonaws.${data.aws_region.current.name}.s3"
         vpc_endpoint_type = "Gateway" # S3 uses gateway endpoints which are free

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-s3-vpc-endpoint"
                  Environment = var.env
         })
  
}
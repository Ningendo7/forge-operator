resource "aws_vpc_endpoint" "forge-interface-endpoints" {

  for_each = toset([
    "ecr.api",
    "ecr.dkr",
    "ec2",
    "logs", # Allows Cloudwatch agent/fluent-bit to skip NAT
    "sts",
  ])

  vpc_id            = var.vpc_id
  service_name      = "com.amazonaws.${data.aws_region.current.region}.${each.key}"
  vpc_endpoint_type = "Interface"

  security_group_ids  = [aws_security_group.forge-vpc-endpoints.id]
  subnet_ids          = var.private_subnet_ids
  private_dns_enabled = true

  tags = merge(var.tags, {
    Name        = "${var.env}-forgecluster-${each.key}-vpc-endpoint"
    Environment = var.env
  })
}

data "aws_region" "current" {}


resource "aws_vpc_endpoint" "s3" {
  vpc_id            = var.vpc_id
  service_name      = "com.amazonaws.${data.aws_region.current.region}.s3"
  vpc_endpoint_type = "Gateway" # S3 uses gateway endpoints which are free

  # Without this, the endpoint exists but nothing actually uses it: a Gateway
  # endpoint only takes effect on route tables it's explicitly associated
  # with (unlike Interface endpoints, which work via private DNS). S3 traffic
  # still works either way -- it just falls back to going out through the
  # NAT gateway, which costs data-processing charges the free Gateway
  # endpoint is meant to avoid.
  route_table_ids = var.private_route_table_ids

  tags = merge(var.tags, {
    Name        = "${var.env}-forgecluster-s3-vpc-endpoint"
    Environment = var.env
  })

}
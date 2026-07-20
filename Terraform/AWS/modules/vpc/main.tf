resource "aws_vpc" "forgecluster_vpc" {
         cidr_block           = var.vpc_cidr
         enable_dns_support   = true
         enable_dns_hostnames = true

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-vpc"
                  Environment = var.env
         })
}

resource "aws_internet_gateway" "vpc-igw" {
         vpc_id = aws_vpc.forgecluster_vpc.id

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-igw"
                  Environment = var.env
         })
  
}

resource "aws_subnet" "public-sbnt" {

         for_each = var.az_network_config

         vpc_id            = aws_vpc.forgecluster_vpc.id
         cidr_block        = each.value.public_subnet_cidr
         availability_zone = each.key
         map_public_ip_on_launch = true

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-public-subnet-${each.key}"
                  Environment = var.env
                  "kubernetes.io/cluster/${var.cluster_name}" = "shared"
                  "kubernetes.io/role/elb" = "1"
         })
}

resource "aws_subnet" "private-sbnt" {

         for_each          = var.az_network_config
         vpc_id            = aws_vpc.forgecluster_vpc.id
         cidr_block        = each.value.private_subnet_cidr
         availability_zone = each.key

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-private-subnet-${each.key}"
                  Environment = var.env
                  "kubernetes.io/cluster/${var.cluster_name}" = "shared"
                  "kubernetes.io/role/internal-elb" = "1"
         })
}

resource "aws_eip" "eip-nat" {

         for_each = var.az_network_config
         domain = "vpc"

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-nat-eip-${each.key}"
                  Environment = var.env
         })
}

resource "aws_nat_gateway" "forge-nat" {

         for_each = var.az_network_config
         allocation_id = aws_eip.eip-nat[each.key].id
         subnet_id     = aws_subnet.public-sbnt[each.key].id

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-nat-gateway-${each.key}"
                  Environment = var.env
         })

         depends_on = [aws_internet_gateway.vpc-igw]
}

resource "aws_route_table" "public-rt" {

         for_each = var.az_network_config
         vpc_id = aws_vpc.forgecluster_vpc.id

         route {
                  cidr_block = "0.0.0.0/0"
                  gateway_id = aws_internet_gateway.vpc-igw.id
         }

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-public-rt-${each.key}"
                  Environment = var.env
         })
}

resource "aws_route_table_association" "public-rt-assoc" {
         for_each       = var.az_network_config
         subnet_id      = aws_subnet.public-sbnt[each.key].id
         route_table_id = aws_route_table.public-rt[each.key].id
}

# One private route table per AZ when using per-AZ NAT gateways, one shared
# table when using a single NAT gateway.
locals {
  private_rt_count = var.enable_single_nat_gateway ? 1 : length(var.az_network_config)
}

resource "aws_route_table" "private-rt" {

         for_each = var.enable_single_nat_gateway ? { "single" = true } : var.az_network_config
         vpc_id = aws_vpc.forgecluster_vpc.id

         route {
                  cidr_block     = "0.0.0.0/0"
                  nat_gateway_id = aws_nat_gateway.forge-nat[each.key].id
         }

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-private-rt-${each.key}"
                  Environment = var.env
         })
}

resource "aws_route_table_association" "private-rt-assoc" {
         for_each       = var.az_network_config
         subnet_id      = aws_subnet.private-sbnt[each.key].id
         route_table_id = aws_route_table.private-rt[var.enable_single_nat_gateway ? "single" : each.key].id
}
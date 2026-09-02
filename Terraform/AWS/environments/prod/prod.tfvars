environment     = "prod"
project_name    = "forge-operator"
aws_region      = "us-east-1"
cluster_name    = "forgecluster"
cluster_version = "1.31"

# VPC Configuration
# Deliberately a distinct range from dev's 10.0.0.0/16, so the two VPCs never
# collide if they're ever peered or put behind a Transit Gateway later.
vpc_cidr = "10.1.0.0/16"
az_network_config = {
  "us-east-1a" = {
    public_subnet_cidr  = "10.1.1.0/24"
    private_subnet_cidr = "10.1.11.0/24"
  }
  "us-east-1b" = {
    public_subnet_cidr  = "10.1.2.0/24"
    private_subnet_cidr = "10.1.12.0/24"
  }
}
enable_single_nat_gateway = false # Multi-AZ: prod shouldn't trade resilience for cost here.

# API server endpoint access: private (in-VPC) only. Reach it via a bastion
# or VPN, not a public endpoint -- unlike dev, there's no "just testing"
# excuse here.
enable_cluster_public_access = false
cluster_public_access_cidrs  = []

# EKS Node Group Configuration
desired_node_capacity = 3
node_min_capacity     = 2
node_max_capacity     = 10
node_max_unavailable  = 1
node_ami_type         = "AL2_x86_64"
node_instance_types   = ["t3.large"]
node_capacity_type    = "ON_DEMAND"

# Optional Features
enable_vpc_cni_irsa = true # Prod uses the scoped IRSA role rather than blanket node-role permissions.
enable_monitoring   = false

tags = {
  Team = "platform"
}

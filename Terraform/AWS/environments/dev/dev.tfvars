environment     = "dev"
project_name    = "forge-operator"
aws_region      = "us-east-1"
cluster_name    = "forgecluster"
cluster_version = "1.31"

# VPC Configuration
vpc_cidr = "10.0.0.0/16"
az_network_config = {
  "us-east-1a" = {
    public_subnet_cidr  = "10.0.1.0/24"
    private_subnet_cidr = "10.0.11.0/24"
  }
  "us-east-1b" = {
    public_subnet_cidr  = "10.0.2.0/24"
    private_subnet_cidr = "10.0.12.0/24"
  }
}
enable_single_nat_gateway = false # Multi-AZ by default

# API server endpoint access
# Open to any IP rather than scoped to one, a deliberate dev-only trade-off
# so kubectl doesn't break every time the connecting IP changes. Real access
# still requires valid AWS IAM credentials mapped to cluster access (EKS
# access entries / aws-auth) -- this only controls network reachability, not
# who can actually do anything once connected. prod stays fully private
# (see prod.tfvars) since "just testing" isn't a good enough reason there.
enable_cluster_public_access = true
cluster_public_access_cidrs  = ["0.0.0.0/0"]

# EKS Node Group Configuration
desired_node_capacity = 2
node_min_capacity     = 1
node_max_capacity     = 5
node_max_unavailable  = 1
node_ami_type         = "AL2_x86_64"
node_instance_types   = ["t3.medium"]
node_capacity_type    = "ON_DEMAND"

# Optional Features
enable_vpc_cni_irsa = false
enable_monitoring   = false

tags = {
  Team = "platform"
}

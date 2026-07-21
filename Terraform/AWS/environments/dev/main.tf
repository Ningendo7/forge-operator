# Root composition for dev environment
# This file orchestrates all modules and shows the dependency flow
# Dependencies flow: VPC → Networking → IAM → EKS

# Module 1: VPC (creates subnet infrastructure)
# No dependencies
module "vpc" {
  source = "../../modules/vpc"

         env       = var.environment
         cluster_name = var.cluster_name
         vpc_cidr  = var.vpc_cidr
         az_network_config = var.az_network_config
         enable_single_nat_gateway = var.enable_single_nat_gateway
  
         tags = local.common_tags
}

# Module 2: Networking (creates security groups, needs VPC)
# Depends on: VPC module (needs vpc_id)
module "networking" {
         source = "../../modules/networking"

         env              = var.environment
         vpc_id           = module.vpc.vpc_id
         private_subnet_ids = module.vpc.private_subnet_ids
  
         tags = local.common_tags
}

# Module 3: IAM (creates cluster and node roles, no infrastructure dependencies)
module "iam" {
         source = "../../modules/iam"

         env          = var.environment
         cluster_name = var.cluster_name
  
         tags = local.common_tags
}

# Module 4: EKS (creates cluster, needs VPC, Networking, and IAM)
# Depends on: VPC (private_subnet_ids), Networking (cluster_security_group_id), IAM (role ARNs)
module "eks" {
         source = "../../modules/eks"

         env                       = var.environment
         cluster_name              = var.cluster_name
         cluster_version           = var.cluster_version
  
         # From IAM module
         cluster_role_arn          = module.iam.cluster_role_arn
         node_role_arn             = module.iam.node_role_arn
  
         # From VPC module
         private_subnet_ids        = module.vpc.private_subnet_ids
  
         # From Networking module
         cluster_security_group_id = module.networking.node_security_group_id
  
         # Node group sizing
         desired_node_capacity     = var.desired_node_capacity
         node_min_capacity         = var.node_min_capacity
         node_max_capacity         = var.node_max_capacity
         node_max_unavailable      = var.node_max_unavailable
         node_ami_type             = var.node_ami_type
         node_instance_types       = var.node_instance_types
         node_capacity_type        = var.node_capacity_type
  
         tags = local.common_tags
}

# Module 5: IRSA 
module "irsa" {

         source = "../../modules/irsa"
         role_name = "dev-app-operator-irsa-role"

         oidc_providers = {
                  (module.eks.oidc_provider_url) = {
                           provider_arn = module.eks.oidc_provider_arn
                           namespace_service_accounts = {
                                    "operators" = ["app-controller-sa"]
                           }
                  }
         }
         
         policy_arns = [
                  aws_iam_policy.s3operator_permissions.arn
         ]
  
         tags = local.common_tags
}

# Module 6: Monitoring (creates observability resources, optional)
# Depends on: EKS (needs cluster endpoint, OIDC info)
# module "monitoring" {
#   count  = var.enable_monitoring ? 1 : 0
#   source = "../../modules/monitoring"
#
#   env              = var.environment
#   cluster_name     = var.cluster_name
#   cluster_endpoint = module.eks.cluster_endpoint
#   oidc_provider_arn = module.eks.oidc_provider_arn
#   oidc_provider_url = module.eks.oidc_provider_url
#
#   tags = local.common_tags
# }

# Local values for consistent tagging
locals {
         common_tags = merge(var.tags, {
                  Environment = var.environment
                  Project     = var.project_name
                  ManagedBy   = "Terraform"
  })
}

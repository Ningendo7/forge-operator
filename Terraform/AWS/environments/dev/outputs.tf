# Dev environment outputs
# Reference these values to connect to the cluster, configure kubectl, etc.

output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = module.vpc.public_subnet_ids
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = module.vpc.private_subnet_ids
}

output "cluster_name" {
  description = "EKS cluster name"
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "EKS cluster API endpoint"
  value       = module.eks.cluster_endpoint
}

output "cluster_ca_certificate" {
  description = "EKS cluster CA certificate (base64 encoded)"
  value       = module.eks.cluster_certificate_authority_data
  sensitive   = true
}

output "oidc_provider_arn" {
  description = "OIDC provider ARN (for IRSA role binding)"
  value       = module.eks.oidc_provider_arn
}

output "oidc_provider_url" {
  description = "OIDC provider URL (for IRSA configuration)"
  value       = module.eks.oidc_provider_url
}

output "cluster_role_arn" {
  description = "IAM role ARN for cluster control plane"
  value       = module.iam.cluster_role_arn
}

output "node_role_arn" {
  description = "IAM role ARN for worker nodes"
  value       = module.iam.node_role_arn
}

output "node_security_group_id" {
  description = "Security group ID for EKS worker nodes"
  value       = module.networking.node_security_group_id
}

# IRSA outputs (only if enabled)
output "vpc_cni_role_arn" {
  description = "IAM role ARN for VPC CNI add-on (IRSA)"
  value       = try(module.irsa[0].vpc_cni_role_arn, null)
}

# Configure kubectl
output "configure_kubectl" {
  description = "Command to configure kubectl"
  value       = "aws eks update-kubeconfig --name ${module.eks.cluster_name} --region ${var.aws_region}"
}

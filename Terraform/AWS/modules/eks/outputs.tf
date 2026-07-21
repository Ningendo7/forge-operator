output "cluster_name" {
         
          description = "The name of the Forge cluster"
          value       = aws_eks_cluster.forge-cluster.name
  
}

output "cluster_endpoint" {
         
          description = "The endpoint of the Forge cluster"
          value       = aws_eks_cluster.forge-cluster.endpoint
  
}

output "cluster_certificate_authority_data" {
         
          description = "The certificate authority data for the Forge cluster"
          value       = aws_eks_cluster.forge-cluster.certificate_authority[0].data
  
}

output "oidc_provider_url" {
         
          description = "The OIDC provider URL for the Forge cluster"
          value       = aws_eks_cluster.forge-cluster.identity[0].oidc[0].issuer
  
}

output "oidc_provider_arn" {
         
          description = "The ARN of the OIDC provider for the Forge cluster"
          value       = aws_iam_openid_connect_provider.forge-oidc.arn
  
}
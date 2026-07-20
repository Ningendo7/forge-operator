output "cluster_role_arn" {
         
          description = "The ARN of the IAM role for the Forge cluster"
          value       = aws_iam_role.forge-cluster-iam.arn
  
}

output "node_role_arn" {
         
          description = "The ARN of the IAM role for the Forge cluster node group"
          value       = aws_iam_role.forge-nodes-iam.arn
  
}
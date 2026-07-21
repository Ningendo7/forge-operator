output "role_arn" {

         description = "The ARN of the IAM role (service account) for the IRSA role"
         value       = aws_iam_role.irsa_role.arn
  
}

output "role_name" {

         description = "The name of the IAM role (service account) for the IRSA role"
         value       = aws_iam_role.irsa_role.name
  
}
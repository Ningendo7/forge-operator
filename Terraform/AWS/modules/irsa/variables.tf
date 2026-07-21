variable "role_name" {
         
         description = "The name of the IAM role for the EKS cluster"
         type        = string
  
}

variable "role_description" {
         
         description = "The description of the IAM role for the EKS cluster"
         type        = string
         default     = "IRSA role created via Terraform"
  
}

variable "oidc_providers" {

         description = "Map of OIDC provider ARNs to their corresponding URL for the EKS cluster"
         type        = map(object({
                  provider_arn = string
                  namespace_service_accounts = map(list(string))
         }))
}
variable "policy_arns" {
         
         description = "A list of IAM policy ARNs to attach to the IRSA role"
         type        = list(string)
         default     = []
  
}

variable "tags" {
         
         description = "A map of tags to apply to all resources"
         type        = map(string)
         default     = {}
  
}
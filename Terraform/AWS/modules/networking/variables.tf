variable "env" {
         
         description = "The environment name (e.g., dev, staging, prod)"
         type        = string
  
}

variable "vpc_id" {
         
         description = "The ID of the VPC created by the VPC module"
         type        = string
  
}

variable "private_subnet_ids" {
         
         description = "The IDs of the private subnets created by the VPC module"
         type        = list(string)
  
}

variable "tags" {

         description = "A map of tags to apply to all resources"
         type        = map(string)
         default     = {}

}
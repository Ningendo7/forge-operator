variable "vpc_label" {

         description = "The name of the VPC."
         type        = string
         default     = null
  
}

variable "vpc_region" {

         description = "The region where the VPC will be created."
         type        = string
         default     = "us-east"
  
}

variable "subnet_label" {

         description = "The name of the subnet."
         type        = string

}

variable "subnet_ip_range" {

         description = "The IP range of the subnet in CIDR notation."
         type        = string
         default     = "10.0.1.0/24"
  
}
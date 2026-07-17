variable "cluster_name" {

         description = "The name of the LKE cluster."
         type        = string
  
}

variable "region" {

         description = "The region where the LKE cluster will be created."
         type        = string
         default     = "us-east"

}

variable "min_nodes" {

         description = "The minimum number of worker nodes in the LKE cluster."
         type        = number
         default     = 3

  validation {
         condition     = var.min_nodes >= 1
         error_message = "The min_nodes must be greater than or equal to 1."
         }
}

variable "max_nodes" {

         description = "The maximum number of worker nodes in the LKE cluster."
         type        = number
         default     = 5

  validation {
         condition     = var.max_nodes >= var.min_nodes
         error_message = "The max_nodes must be greater than or equal to min_nodes."
         }
}



variable "node_type" {

         description = "The type/size of the nodes in the LKE cluster."
         type        = string
         default     = "g6-standard-2"

}

variable "kubernetes_version" {

         description = "The version of Kubernetes to use for the LKE cluster."
         type        = string
         default     = "1.36"

}

variable "tags" {

         description = "A map of tags to assign to the LKE cluster."
         type        = map(string)
         default     = {}
}

variable "enable_ha" {
         description = "Whether to enable high availability for the LKE cluster."
         type        = bool
         default     = false
  
}

variable "subnet_id" {
         description = "The VPC Subnet ID to assign to the LKE cluster nodes."
         type        = number
  
}
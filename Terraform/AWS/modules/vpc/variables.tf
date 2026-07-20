variable "env" {

         description = "The environment name (e.g., dev, staging, prod)"
         type        = string

}

variable "vpc_cidr" {

         description = "The CIDR block for the VPC"
         type        = string
         default     = "10.0.0.0/16"

}

variable "az_network_config" {
         
         description = "Map of availability zones to their corresponding public and private subnet CIDR blocks"
         type        = map(object({
                  public_subnet_cidr  = string
                  private_subnet_cidr = string
         }))
  
}

variable "cluster_name" {

         description = "The name of the cluster"
         type        = string

}

variable "enable_single_nat_gateway" {

         description = "Whether to enable a single NAT gateway for the VPC"
         type        = bool
         default     = false

}

variable "tags" {

         description = "A map of tags to apply to all resources"
         type        = map(string)
         default     = {}

}

variable "enable_flow_logs" {

         description = "Whether to enable VPC Flow Logs"
         type        = bool
         default     = true

}

variable "flow_log_retention_days" {

         description = "The number of days to retain VPC Flow Logs"
         type        = number
         default     = 30

}
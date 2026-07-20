variable "env" {
         
         description = "The environment name (e.g., dev, staging, prod)"
         type        = string

}

variable "cluster_name" {

         description = "The name of the cluster"
         type        = string

}

variable "tags" {
         
         description = "A map of tags to apply to all resources"
         type        = map(string)
         default     = {}
}
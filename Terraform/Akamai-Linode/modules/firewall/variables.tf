variable "firewall_label" {

         description = "The name of the Cloud firewall."
         type        = string
  
}

variable "linode_ids" {

         description = "A list of Linode IDs to which the firewall will be applied."
         type        = list(number)
         default     = []
  
}
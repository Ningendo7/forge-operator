variable "firewall_label" {

  description = "The name of the Cloud firewall."
  type        = string

}

variable "linode_ids" {

  description = "A list of Linode IDs to which the firewall will be applied."
  type        = list(number)
  default     = []

}

variable "ssh_allowed_cidrs" {

  description = "CIDR blocks allowed to reach port 22. Leave empty to omit the SSH rule entirely rather than defaulting to the whole internet."
  type        = list(string)
  default     = []

}
# Variable schema for this environment. No defaults here on purpose: every
# value is supplied explicitly via this environment's own *.tfvars file (terraform apply
# -var-file=<env>.tfvars), so this file stays identical across environments
# and there's nowhere for the two environments' config to silently drift.

# Networking
variable "vpc_label" {
  description = "The name of the VPC"
  type        = string
}

variable "vpc_region" {
  description = "The region where the VPC will be created"
  type        = string
}

variable "subnet_label" {
  description = "The name of the subnet"
  type        = string
}

# LKE Cluster
variable "cluster_name" {
  description = "The name of the LKE cluster"
  type        = string
}

variable "region" {
  description = "The region where the LKE cluster will be created"
  type        = string
}

variable "node_type" {
  description = "The type/size of the nodes in the LKE cluster"
  type        = string
}

variable "kubernetes_version" {
  description = "The version of Kubernetes to use for the LKE cluster"
  type        = string
}

variable "enable_ha" {
  description = "Whether to enable high availability for the LKE cluster control plane"
  type        = bool
}

variable "min_nodes" {
  description = "The minimum number of worker nodes in the LKE cluster"
  type        = number
}

variable "max_nodes" {
  description = "The maximum number of worker nodes in the LKE cluster"
  type        = number
}

# Firewall
variable "firewall_label" {
  description = "The name of the Cloud firewall"
  type        = string
}

variable "ssh_allowed_cidrs" {
  description = "CIDR blocks allowed to reach port 22. Leave empty to omit the SSH rule entirely rather than defaulting to the whole internet."
  type        = list(string)
}

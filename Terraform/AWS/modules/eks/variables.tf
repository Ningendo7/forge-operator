variable "env" {

  description = "The environment name (e.g., dev, staging, prod)"
  type        = string

}

variable "cluster_name" {

  description = "The name of the cluster"
  type        = string

}

variable "cluster_version" {

  description = "The version of the EKS cluster"
  type        = string
  default     = "1.31"

}

variable "cluster_role_arn" {

  description = "The ARN of the IAM role for the EKS cluster"
  type        = string

  validation {
    condition     = can(regex("^arn:aws:iam::[0-9]{12}:role/", var.cluster_role_arn))
    error_message = "cluster_role_arn must be a valid IAM role ARN (e.g., arn:aws:iam::123456789012:role/eks-cluster-role)."
  }
}

variable "node_role_arn" {

  description = "The ARN of the IAM role for the EKS node group"
  type        = string

}

variable "private_subnet_ids" {

  description = "A list of private subnet IDs for the EKS cluster"
  type        = list(string)

}

variable "enable_cluster_public_access" {

  description = "Whether the EKS API server endpoint is reachable from outside the VPC. Private access (in-VPC) is always on regardless of this. Defaults to false: kubectl from outside the VPC (a bastion, a laptop not on a VPN, etc.) needs this enabled and cluster_public_access_cidrs scoped to wherever it's connecting from, or it'll hang trying to reach a private-only endpoint it has no route to."
  type        = bool
  default     = false

}

variable "cluster_public_access_cidrs" {

  description = "The CIDR blocks that are allowed to access the EKS cluster publicly. Only takes effect when enable_cluster_public_access is true. Keep this scoped (e.g. your own IP as a /32), not 0.0.0.0/0 -- this is the EKS control plane's own API, not just an app endpoint."
  type        = list(string)
  default     = []

}

variable "cluster_security_group_id" {

  description = "The ID of the security group for the EKS cluster"
  type        = string

}

variable "desired_node_capacity" {

  description = "The desired number of nodes in the EKS node group"
  type        = number
  default     = 3

}

variable "node_max_capacity" {

  description = "The maximum number of nodes in the EKS node group"
  type        = number
  default     = 5

}

variable "node_min_capacity" {

  description = "The minimum number of nodes in the EKS node group"
  type        = number
  default     = 1

}

variable "node_max_unavailable" {

  description = "The maximum number of nodes that can be unavailable during an update"
  type        = number
  default     = 1

}

variable "node_ami_type" {

  description = "The AMI type for the EKS node group (e.g., AL2_x86_64, AL2_ARM_64)"
  type        = string
  default     = "AL2_x86_64"

}

variable "node_instance_types" {

  description = "A list of instance types for the EKS node group"
  type        = list(string)
  default     = ["t3.medium"]

}

variable "node_capacity_type" {

  description = "The capacity type for the EKS node group (e.g., ON_DEMAND, SPOT)"
  type        = string
  default     = "ON_DEMAND"

}

variable "tags" {

  description = "A map of tags to apply to all resources"
  type        = map(string)
  default     = {}

}
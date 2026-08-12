# Variable schema for this environment. No defaults here on purpose: every
# value is supplied explicitly via this environment's own *.tfvars file (terraform apply
# -var-file=<env>.tfvars), so this file stays identical across environments
# and there's nowhere for the two environments' config to silently drift.

variable "environment" {
  description = "Environment name"
  type        = string
}

variable "project_name" {
  description = "Project name for resource tagging"
  type        = string
}

variable "aws_region" {
  description = "AWS region"
  type        = string
}

variable "cluster_name" {
  description = "EKS cluster name"
  type        = string
}

variable "cluster_version" {
  description = "EKS cluster Kubernetes version"
  type        = string
}

# VPC Configuration
variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
}

variable "az_network_config" {
  description = "Availability zone network configuration (public and private subnets per AZ)"
  type = map(object({
    public_subnet_cidr  = string
    private_subnet_cidr = string
  }))
}

variable "enable_single_nat_gateway" {
  description = "Use a single NAT gateway for all private subnets (cost savings, single point of failure)"
  type        = bool
}

# EKS Node Group Configuration
variable "desired_node_capacity" {
  description = "Desired number of nodes"
  type        = number
}

variable "node_min_capacity" {
  description = "Minimum number of nodes"
  type        = number
}

variable "node_max_capacity" {
  description = "Maximum number of nodes (for autoscaling)"
  type        = number
}

variable "node_max_unavailable" {
  description = "Max nodes unavailable during updates"
  type        = number
}

variable "node_ami_type" {
  description = "AMI type for nodes (AL2_x86_64, AL2_ARM_64, etc.)"
  type        = string
}

variable "node_instance_types" {
  description = "EC2 instance types for node group"
  type        = list(string)
}

variable "node_capacity_type" {
  description = "Capacity type: ON_DEMAND or SPOT"
  type        = string
}

# Optional Features
variable "enable_vpc_cni_irsa" {
  description = "Enable IRSA for VPC CNI (allows fine-grained IAM permissions)"
  type        = bool
}

variable "enable_monitoring" {
  description = "Enable CloudWatch monitoring stack"
  type        = bool
}

variable "tags" {
  description = "Common tags for all resources"
  type        = map(string)
}

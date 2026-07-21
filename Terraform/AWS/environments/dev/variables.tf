# Dev environment variables
# These feed into the root composition in main.tf

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "project_name" {
  description = "Project name for resource tagging"
  type        = string
  default     = "forge-operator"
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "cluster_name" {
  description = "EKS cluster name"
  type        = string
  default     = "forgecluster"
}

variable "cluster_version" {
  description = "EKS cluster Kubernetes version"
  type        = string
  default     = "1.31"
}

# VPC Configuration
variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_network_config" {
  description = "Availability zone network configuration (public and private subnets per AZ)"
  type = map(object({
    public_subnet_cidr  = string
    private_subnet_cidr = string
  }))
  default = {
    "us-east-1a" = {
      public_subnet_cidr  = "10.0.1.0/24"
      private_subnet_cidr = "10.0.11.0/24"
    }
    "us-east-1b" = {
      public_subnet_cidr  = "10.0.2.0/24"
      private_subnet_cidr = "10.0.12.0/24"
    }
  }
}

variable "enable_single_nat_gateway" {
  description = "Use a single NAT gateway for all private subnets (cost savings, single point of failure)"
  type        = bool
  default     = false  # Multi-AZ by default
}

# EKS Node Group Configuration
variable "desired_node_capacity" {
  description = "Desired number of nodes"
  type        = number
  default     = 2
}

variable "node_min_capacity" {
  description = "Minimum number of nodes"
  type        = number
  default     = 1
}

variable "node_max_capacity" {
  description = "Maximum number of nodes (for autoscaling)"
  type        = number
  default     = 5
}

variable "node_max_unavailable" {
  description = "Max nodes unavailable during updates"
  type        = number
  default     = 1
}

variable "node_ami_type" {
  description = "AMI type for nodes (AL2_x86_64, AL2_ARM_64, etc.)"
  type        = string
  default     = "AL2_x86_64"
}

variable "node_instance_types" {
  description = "EC2 instance types for node group"
  type        = list(string)
  default     = ["t3.medium"]
}

variable "node_capacity_type" {
  description = "Capacity type: ON_DEMAND or SPOT"
  type        = string
  default     = "ON_DEMAND"
}

# Optional Features
variable "enable_vpc_cni_irsa" {
  description = "Enable IRSA for VPC CNI (allows fine-grained IAM permissions)"
  type        = bool
  default     = false
}

variable "enable_monitoring" {
  description = "Enable CloudWatch monitoring stack"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Common tags for all resources"
  type        = map(string)
  default = {
    Team = "platform"
  }
}

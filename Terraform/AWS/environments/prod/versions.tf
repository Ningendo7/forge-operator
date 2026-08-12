terraform {

  # use_lockfile (native S3 state locking, no DynamoDB) requires >= 1.10.0.
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    # Native state locking
    use_lockfile = true
  }
}

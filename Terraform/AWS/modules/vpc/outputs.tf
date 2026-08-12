output "vpc_id" {
  description = "The ID of the VPC"
  value       = aws_vpc.forgecluster_vpc.id

}

output "public_subnet_ids" {
  description = "The IDs of the public subnets"
  # public-sbnt uses for_each, so it's a map: [*] alone doesn't splat it the
  # way it would a count-based resource. values() converts it to a list first.
  value = values(aws_subnet.public-sbnt)[*].id
}

output "private_subnet_ids" {
  description = "The IDs of the private subnets"
  value       = values(aws_subnet.private-sbnt)[*].id
}

output "vpc_cidr_block" {
  description = "The CIDR block of the VPC"
  value       = aws_vpc.forgecluster_vpc.cidr_block
}
output "subnet_id" {
  description = "The ID of the VPC subnet the LKE cluster nodes run in."
  value       = module.dev_network.subnet_id
}

output "cluster_id" {
  description = "The ID of the LKE cluster."
  value       = module.dev_kubernetes.cluster_id
}

output "cluster_endpoint" {
  description = "The API endpoint of the LKE cluster."
  value       = module.dev_kubernetes.cluster_endpoint
}

output "kubeconfig" {
  description = "The base64-encoded kubeconfig for the LKE cluster."
  value       = module.dev_kubernetes.kubeconfig
  sensitive   = true
}

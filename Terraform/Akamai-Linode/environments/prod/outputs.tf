output "subnet_id" {
  description = "The ID of the VPC subnet the LKE cluster nodes run in."
  value       = module.prod_network.subnet_id
}

output "cluster_id" {
  description = "The ID of the LKE cluster."
  value       = module.prod_kubernetes.cluster_id
}

output "cluster_endpoint" {
  description = "The API endpoint of the LKE cluster."
  value       = module.prod_kubernetes.cluster_endpoint
}

output "kubeconfig" {
  description = "The base64-encoded kubeconfig for the LKE cluster."
  value       = module.prod_kubernetes.kubeconfig
  sensitive   = true
}

output "firewall_id" {
  description = "The ID of the firewall attached to the LKE node pool."
  value       = module.prod_firewall.id
}

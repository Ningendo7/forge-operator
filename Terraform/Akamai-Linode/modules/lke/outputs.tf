output "cluster_id" {

  description = "The ID of the LKE cluster."
  value       = linode_lke_cluster.lke-cluster.id

}

output "cluster_endpoint" {

  description = "The endpoint of the LKE cluster."
  value       = linode_lke_cluster.lke-cluster.api_endpoints[0]

}

output "kubeconfig" {

  description = "The base64-encoded kubeconfig file to connect to the LKE cluster via kubectl."
  value       = linode_lke_cluster.lke-cluster.kubeconfig
  sensitive   = true

}
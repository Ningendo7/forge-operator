output "id" {
  description = "The ID of the firewall, for attaching to an LKE node pool's firewall_id."
  value       = linode_firewall.lke_firewall.id
}

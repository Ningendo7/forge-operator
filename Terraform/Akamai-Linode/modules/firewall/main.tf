resource "linode_firewall" "lke_firewall" {

  label = var.firewall_label

  # Block all inbound traffic unless explicitly allowed by rules
  inbound_policy = "DROP"

  # Allow all outbound traffic unless explicitly denied by rules
  outbound_policy = "ACCEPT"

  # Rule 1: Allow inbound SSH only from explicitly configured CIDRs. No rule
  # is created at all if ssh_allowed_cidrs is empty, rather than defaulting
  # to the entire internet.
  dynamic "inbound" {
    for_each = length(var.ssh_allowed_cidrs) > 0 ? [1] : []
    content {
      label    = "Allow-SSH"
      action   = "ACCEPT"
      protocol = "TCP"
      ports    = "22"
      ipv4     = var.ssh_allowed_cidrs
    }
  }

  # Rule 2: Allow standard web traffic (HTTP Port 80)
  inbound {

    label    = "Allow-HTTP"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "80"
    ipv4     = ["0.0.0.0/0"]

  }

  # Rule 3: Allow secure web traffic (HTTPS Port 443)
  inbound {

    label    = "Allow-HTTPS"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "443"
    ipv4     = ["0.0.0.0/0"]

  }

  #Dynamiclly assign to compute instances if provided, otherwise assign to the VPC
  linodes = var.linode_ids
}
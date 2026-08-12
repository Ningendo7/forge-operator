# 1. Spin up the network

module "prod_network" {

  source = "../../modules/networking"

  vpc_label    = var.vpc_label
  vpc_region   = var.vpc_region
  subnet_label = var.subnet_label

}

# 2. Secure your resources with the firewall (created before the cluster so
# its ID can be attached to the node pool below).
module "prod_firewall" {

  source = "../../modules/firewall"

  firewall_label = var.firewall_label

  # No SSH access by default. Add your admin/bastion CIDR(s) here if you need
  # it, e.g. ssh_allowed_cidrs = ["203.0.113.4/32"].
  ssh_allowed_cidrs = var.ssh_allowed_cidrs

}

# 3. Spin up the Kubernetes cluster inside the subnet created above, with the
# firewall attached to the node pool.
module "prod_kubernetes" {

  source = "../../modules/lke"

  cluster_name       = var.cluster_name
  region             = var.region
  node_type          = var.node_type
  kubernetes_version = var.kubernetes_version

  # Prod runs a highly-available control plane across multiple availability zones.
  enable_ha = var.enable_ha

  # Configured auto-scaling for the worker nodes. The cluster will automatically scale between min_nodes and max_nodes based on the workload.
  min_nodes = var.min_nodes
  max_nodes = var.max_nodes

  subnet_id   = module.prod_network.subnet_id
  firewall_id = module.prod_firewall.id

}

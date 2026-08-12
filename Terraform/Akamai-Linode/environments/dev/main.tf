# 1. Spin up the network

module "dev_network" {

  source = "../../modules/networking"

  vpc_label    = var.vpc_label
  vpc_region   = var.vpc_region
  subnet_label = var.subnet_label

}

# 2. Spin up the Kubernetes cluster inside the subnet created in the previous step.
module "dev_kubernetes" {

  source = "../../modules/lke"

  cluster_name       = var.cluster_name
  region             = var.region
  node_type          = var.node_type
  kubernetes_version = var.kubernetes_version

  # Can be set to true if you want to enable high availability for the control plane. This will create multiple control plane nodes across different availability zones.
  enable_ha = var.enable_ha

  # Configured auto-scaling for the worker nodes. The cluster will automatically scale between min_nodes and max_nodes based on the workload.
  min_nodes = var.min_nodes
  max_nodes = var.max_nodes

  subnet_id = module.dev_network.subnet_id

}

# 3. Secure your resources with the firewall
module "dev_firewall" {

  source = "../../modules/firewall"

  firewall_label    = var.firewall_label
  ssh_allowed_cidrs = var.ssh_allowed_cidrs

}

# 1. Spin up the network

module "dev_network" {

         source = "../../modules/networking"

         vpc_label    = "dev-vpc"
         vpc_region   = "us-iad"
         subnet_label = "dev-subnet-alpha"

}

# 2. Spin up the Kubernetes cluster inside the subnet created in the previous step.
module "dev_kubernetes" {

         source = "../../modules/lke"

         cluster_name       = "dev-cluster"
         region             = "us-iad"
         node_type          = "g6-standard-2"
         kubernetes_version = "1.36"

         # Can be set to true if you want to enable high availability for the control plane. This will create multiple control plane nodes across different availability zones.
         enable_ha = false

         # Configured auto-scaling for the worker nodes. The cluster will automatically scale between min_nodes and max_nodes based on the workload.
         min_nodes = 2
         max_nodes = 4

         subnet_id = module.dev_network.subnet_id

}

# 3. Secure your resources with the firewall
module "dev_firewall" {

         source = "../../modules/firewall"

         firewall_label = "dev-firewall"

}


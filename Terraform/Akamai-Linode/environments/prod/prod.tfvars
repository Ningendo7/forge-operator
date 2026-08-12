# Networking
vpc_label    = "prod-vpc"
vpc_region   = "us-iad"
subnet_label = "prod-subnet-alpha"

# LKE Cluster
cluster_name       = "prod-cluster"
region             = "us-iad"
node_type          = "g6-standard-4"
kubernetes_version = "1.36"

# Prod runs a highly-available control plane across multiple availability zones.
enable_ha = true

# Configured auto-scaling for the worker nodes. The cluster will automatically scale between min_nodes and max_nodes based on the workload.
min_nodes = 3
max_nodes = 6

# Firewall
firewall_label = "prod-firewall"

# No SSH access by default. Add your admin/bastion CIDR(s) here if you need
# it, e.g. ssh_allowed_cidrs = ["203.0.113.4/32"].
ssh_allowed_cidrs = []

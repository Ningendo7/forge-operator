# Networking
vpc_label    = "dev-vpc"
vpc_region   = "us-iad"
subnet_label = "dev-subnet-alpha"

# LKE Cluster
cluster_name       = "dev-cluster"
region             = "us-iad"
node_type          = "g6-standard-2"
kubernetes_version = "1.36"

# Can be set to true if you want to enable high availability for the control plane. This will create multiple control plane nodes across different availability zones.
enable_ha = false

# Configured auto-scaling for the worker nodes. The cluster will automatically scale between min_nodes and max_nodes based on the workload.
min_nodes = 2
max_nodes = 4

# Firewall
firewall_label = "dev-firewall"

# No SSH access by default. Add your admin/bastion CIDR(s) here if you need
# it, e.g. ssh_allowed_cidrs = ["203.0.113.4/32"].
ssh_allowed_cidrs = []

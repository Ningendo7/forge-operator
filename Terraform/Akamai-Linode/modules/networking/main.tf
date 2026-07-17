resource "linode_vpc" "lke_vpc" {
         label = var.vpc_label
         region = var.vpc_region
         description = "VPC for LKE cluster"
  
}

resource "linode_vpc_subnet" "lke_subnet" {
         label = var.subnet_label
         vpc_id = linode_vpc.lke_vpc.id
         ipv4 = var.subnet_ip_range
  
}
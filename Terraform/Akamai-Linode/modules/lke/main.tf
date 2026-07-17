resource "linode_lke_cluster" "lke-cluster" {
  
         label = var.cluster_name
         region = var.region
         k8s_version = var.kubernetes_version

         control_plane {

                  high_availability = var.enable_ha
         }

         pool {

         type  = var.node_type
         
         autoscaler {
                  min = var.min_nodes
                  max = var.max_nodes
         }

         }


         tags = ["managed-by:forge-operator"]

         subnet_id = var.subnet_id

         # Prevent accidental deletion of the cluster. This is a safety measure to avoid accidental deletion of the cluster.
         lifecycle {

                  prevent_destroy = true

         }
}



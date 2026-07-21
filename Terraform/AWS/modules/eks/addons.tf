# EKS add-on management
# Ensures core add-ons are installed and kept up-to-date with the cluster version

resource "aws_eks_addon" "vpc_cni" {
         cluster_name                    = aws_eks_cluster.forge-cluster.name
         addon_name                      = "vpc-cni"
         addon_version                   = data.aws_eks_addon_version.vpc_cni.version
         service_account_role_arn        = var.vpc_cni_role_arn != "" ? var.vpc_cni_role_arn : null
         resolve_conflicts_on_create     = "OVERWRITE"
         resolve_conflicts_on_update     = "PRESERVE"

         tags = merge(var.tags, {
                  Name = "${var.env}-${var.cluster_name}-vpc-cni"
         })

         depends_on = [aws_eks_node_group.forge-nodes]
}


resource "aws_eks_addon" "coredns" {
         cluster_name                    = aws_eks_cluster.forge-cluster.name
         addon_name                      = "coredns"
         addon_version                   = data.aws_eks_addon_version.coredns.version
         resolve_conflicts_on_create     = "OVERWRITE"
         resolve_conflicts_on_update     = "PRESERVE"

         tags = merge(var.tags, {
                  Name = "${var.env}-${var.cluster_name}-coredns"
         })

         depends_on = [aws_eks_node_group.forge-nodes]
}

resource "aws_eks_addon" "kube_proxy" {
         cluster_name                    = aws_eks_cluster.forge-cluster.name
         addon_name                      = "kube-proxy"
         addon_version                   = data.aws_eks_addon_version.kube_proxy.version
         resolve_conflicts_on_create     = "OVERWRITE"
         resolve_conflicts_on_update     = "PRESERVE"

         tags = merge(var.tags, {
                  Name = "${var.env}-${var.cluster_name}-kube-proxy"
         })

         depends_on = [aws_eks_node_group.forge-nodes]
}

# Data sources to fetch latest compatible addon versions for the cluster
data "aws_eks_addon_version" "vpc_cni" {
         addon_name             = "vpc-cni"
         kubernetes_version     = aws_eks_cluster.forge-cluster.version
         most_recent            = true
}

data "aws_eks_addon_version" "coredns" {
         addon_name             = "coredns"
         kubernetes_version     = aws_eks_cluster.forge-cluster.version
         most_recent            = true
}

data "aws_eks_addon_version" "kube_proxy" {
         addon_name             = "kube-proxy"
         kubernetes_version     = aws_eks_cluster.forge-cluster.version
         most_recent            = true
}

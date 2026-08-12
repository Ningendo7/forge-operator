# EKS add-on management
# Ensures core add-ons are installed and kept up-to-date with the cluster version
#
# vpc-cni is deliberately NOT managed here: it optionally needs an IRSA role,
# which needs this module's own OIDC provider as input. Feeding that role's
# ARN back into this module would create a circular module dependency, so
# vpc-cni is created at the root instead, after both this module and the
# vpc_cni_irsa module exist.

resource "aws_eks_addon" "coredns" {
  cluster_name                = aws_eks_cluster.forge-cluster.name
  addon_name                  = "coredns"
  addon_version               = data.aws_eks_addon_version.coredns.version
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"

  tags = merge(var.tags, {
    Name = "${var.env}-${var.cluster_name}-coredns"
  })

  depends_on = [aws_eks_node_group.forge-nodes]
}

resource "aws_eks_addon" "kube_proxy" {
  cluster_name                = aws_eks_cluster.forge-cluster.name
  addon_name                  = "kube-proxy"
  addon_version               = data.aws_eks_addon_version.kube_proxy.version
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"

  tags = merge(var.tags, {
    Name = "${var.env}-${var.cluster_name}-kube-proxy"
  })

  depends_on = [aws_eks_node_group.forge-nodes]
}

# Data sources to fetch latest compatible addon versions for the cluster
data "aws_eks_addon_version" "coredns" {
  addon_name         = "coredns"
  kubernetes_version = aws_eks_cluster.forge-cluster.version
  most_recent        = true
}

data "aws_eks_addon_version" "kube_proxy" {
  addon_name         = "kube-proxy"
  kubernetes_version = aws_eks_cluster.forge-cluster.version
  most_recent        = true
}

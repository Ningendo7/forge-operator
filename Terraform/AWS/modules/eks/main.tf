resource "aws_eks_cluster" "forge-cluster" {
  name     = "${var.env}-${var.cluster_name}"
  role_arn = var.cluster_role_arn
  version  = var.cluster_version

  vpc_config {
    subnet_ids              = var.private_subnet_ids
    endpoint_private_access = true
    endpoint_public_access  = var.enable_cluster_public_access
    public_access_cidrs     = var.cluster_public_access_cidrs
    security_group_ids      = [var.cluster_security_group_id]

  }

  enabled_cluster_log_types = [
    "api",
    "audit",
    "authenticator",
    "controllerManager",
    "scheduler",
  ]

  tags = merge(var.tags, {
    Environment = var.env
  })


}

data "tls_certificate" "forge-oidc" {
  url = aws_eks_cluster.forge-cluster.identity[0].oidc[0].issuer

}

resource "aws_iam_openid_connect_provider" "forge-oidc" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.forge-oidc.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.forge-cluster.identity[0].oidc[0].issuer

  tags = merge(var.tags, {
    Environment = var.env
  })
}

## Without this, EKS falls back to its own auto-created default cluster
## security group for the node instances -- a *different* group than
## var.cluster_security_group_id, which is what the control plane's own
## ENIs use (see vpc_config above). The two groups have no rule allowing
## traffic between them, so nodes launch fine at the EC2 level but can
## never actually reach the API server to register as Kubernetes nodes --
## the node group sits at CREATING forever with no error surfaced anywhere.
## Attaching the same security group here lets the existing self-referencing
## "allow all from self" rule on that group (see the networking module's
## forge-nodes-sg) cover node<->control-plane traffic as originally intended
## by that security group's own name and description.
resource "aws_launch_template" "forge_nodes" {
  name_prefix = "${var.env}-${var.cluster_name}-nodes-"

  vpc_security_group_ids = [var.cluster_security_group_id]

  tag_specifications {
    resource_type = "instance"
    tags = merge(var.tags, {
      Environment = var.env
    })
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_eks_node_group" "forge-nodes" {
  cluster_name    = aws_eks_cluster.forge-cluster.name
  node_group_name = "${aws_eks_cluster.forge-cluster.name}-nodes"
  node_role_arn   = var.node_role_arn
  subnet_ids      = var.private_subnet_ids

  scaling_config {

    desired_size = var.desired_node_capacity
    max_size     = var.node_max_capacity
    min_size     = var.node_min_capacity

  }

  update_config {
    max_unavailable = var.node_max_unavailable
  }

  ami_type       = var.node_ami_type
  instance_types = var.node_instance_types
  capacity_type  = var.node_capacity_type

  launch_template {
    id      = aws_launch_template.forge_nodes.id
    version = aws_launch_template.forge_nodes.latest_version
  }

  labels = {
    Environment = var.env
  }

  tags = merge(var.tags, {
    "kubernetes.io/cluster-autoscaler/${aws_eks_cluster.forge-cluster.name}" = "owned"
    "k8s.io/cluster-autoscaler/enabled"                                      = "true"
  })

  depends_on = [aws_eks_cluster.forge-cluster]
}
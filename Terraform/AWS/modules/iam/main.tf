resource "aws_iam_role" "forge-cluster-iam" {
         
         name = "${var.env}-${var.cluster_name}-iam-role"

         assume_role_policy = jsonencode({
                  Version = "2012-10-17"
                  Statement = [
                           {
                                    Action = "sts:AssumeRole"
                                    Effect = "Allow"
                                    Principal = {
                                             Service = "eks.amazonaws.com"
                                    }
                           }
                  ]
         })

         tags = merge(var.tags, {
                  Environment = var.env
         })
  
}

resource "aws_iam_role_policy_attachment" "forge-cluster-iam-policy-attachment" {
         
         policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
         role       = aws_iam_role.forge-cluster-iam.name
}

resource "aws_iam_role" "forge-nodes-iam" {
         
         name = "${var.env}-${var.cluster_name}-nodes-iam-role"

         assume_role_policy = jsonencode({
                  Version = "2012-10-17"
                  Statement = [
                           {
                                    Action = "sts:AssumeRole"
                                    Effect = "Allow"
                                    Principal = {
                                             Service = "ec2.amazonaws.com"
                                    }
                           }
                  ]
         })

         tags = merge(var.tags, {
                  Environment = var.env
         })
  
}

resource "aws_iam_role_policy_attachment" "forge-nodes-iam-policy-attachment" {
         
         policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
         role       = aws_iam_role.forge-nodes-iam.name
}

resource "aws_iam_role_policy_attachment" "forge-nodes-iam-policy-attachment-AmazonEC2ContainerRegistryReadOnly" {
         
         policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
         role       = aws_iam_role.forge-nodes-iam.name
}

resource "aws_iam_role_policy_attachment" "forge-nodes-iam-policy-attachment-AmazonEKS_CNI_Policy" {
         
         policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
         role       = aws_iam_role.forge-nodes-iam.name
}
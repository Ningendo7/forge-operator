resource "aws_security_group" "forge-nodes-sg" {
         name        = "${var.env}-forgecluster-nodes-sg"
         description = "Security group for Forge cluster nodes"
         vpc_id      = var.vpc_id

         # Allow nodes to talk to each other completely
         ingress {
                  description = "Allow node to node communication"
                  from_port   = 0
                  to_port     = 0
                  protocol    = "-1"
                  self        = true
         }

         # Allow outbound traffic to the internet
         egress {
                  description = "Allow outbound traffic to the internet"
                  from_port   = 0
                  to_port     = 0
                  protocol    = "-1"
                  cidr_blocks = ["0.0.0.0/0"]
         }


         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-nodes-sg"
                  Environment = var.env
         })
  
}

// Dedicated security group for the VPC Interface Endpoints
resource "aws_security_group" "forge-vpc-endpoints" {
         name        = "${var.env}-forgecluster-vpc-endpoints-sg"
         description = "Security group for Forge cluster VPC Interface Endpoints"
         vpc_id      = var.vpc_id

         # Allow outbound traffic to endpoints *only* from our EKS worker nodes
         ingress {
                  description = "Allow HTTPS from EKS worker nodes"
                  from_port   = 443
                  to_port     = 443
                  protocol    = tcp
                  security_groups = [aws_security_group.forge-nodes-sg.id]
         }

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-vpc-endpoints-sg"
                  Environment = var.env
         })
}
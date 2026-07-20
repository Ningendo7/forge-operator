resource "aws_iam_role" "flow-logs-role" {

         count = var.enable_flow_logs ? 1 : 0
         name = "${var.env}-forgecluster-flow-logs-role"

         assume_role_policy = jsonencode({
                  Version = "2012-10-17"
                  Statement = [
                           {
                                    Action = "sts:AssumeRole"
                                    Effect = "Allow"
                                    Principal = {
                                             Service = "vpc-flow-logs.amazonaws.com"
                                    }
                           }
                  ]
         })

         tags = merge(var.tags, {
                  Environment = var.env
         })
  
}

resource "aws_iam_role_policy" "flow-logs-policy" {

         count = var.enable_flow_logs ? 1 : 0
         name = "${var.env}-forgecluster-flow-logs-policy"
         role = aws_iam_role.flow-logs-role[0].id

         policy = jsonencode({
                  Version = "2012-10-17"
                  Statement = [
                           {
                                    Action = [
                                             "logs:CreateLogStream",
                                             "logs:PutLogEvents",
                                             "logs:DescribeLogGroups",
                                             "logs:DescribeLogStreams"
                                    ]
                                    Effect   = "Allow"
                                    Resource = "${aws_cloudwatch_log_group.vpc-flow-logs-log-group[0].arn}"
                           }
                  ]
         })
}
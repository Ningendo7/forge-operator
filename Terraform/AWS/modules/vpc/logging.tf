resource "aws_cloudwatch_log_group" "vpc-flow-logs-log-group" {

         count = var.enable_flow_logs ? 1 : 0
         name              = "/aws/vpc/${var.env}-flow-logs"
         retention_in_days = var.flow_log_retention_days

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-vpc-flow-logs"
                  Environment = var.env
         })
}

resource "aws_flow_log" "vpc-flow-logs" {

         count = var.enable_flow_logs ? 1 : 0
         vpc_id         = aws_vpc.forgecluster_vpc.id
         log_destination = aws_cloudwatch_log_group.vpc-flow-logs-log-group[0].arn
         traffic_type   = "ALL"
         iam_role_arn   = aws_iam_role.flow-logs-iam.arn

         tags = merge(var.tags, {
                  Name        = "${var.env}-forgecluster-vpc-flow-logs"
                  Environment = var.env
         })

}

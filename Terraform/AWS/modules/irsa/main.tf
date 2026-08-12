data "aws_iam_policy_document" "irsa_assume_role_policy" {

  dynamic "statement" {

    for_each = var.oidc_providers

    content {
      effect  = "Allow"
      actions = ["sts:AssumeRoleWithWebIdentity"]

      principals {
        type        = "Federated"
        identifiers = [statement.value.provider_arn]
      }
      condition {
        test     = "StringEquals"
        variable = "${replace(statement.value.provider_arn, "https://", "")}:sub"
        values = flatten([
          for ns, sa_list in statement.value.namespace_service_accounts : [
            for sa in sa_list : "system:serviceaccount:${ns}:${sa}"
          ]
        ])
      }
      condition {
        test     = "StringEquals"
        variable = "${replace(statement.value.provider_arn, "https://", "")}:aud"
        values   = ["sts.amazonaws.com"]
      }
    }
  }
}

resource "aws_iam_role" "irsa_role" {
  name               = var.role_name
  description        = var.role_description
  assume_role_policy = data.aws_iam_policy_document.irsa_assume_role_policy.json

  tags = merge(var.tags, {

  })
}

resource "aws_iam_role_policy_attachment" "irsa_policy_attachments" {
  # Keyed by index, not by the ARN itself: policy_arns can include ARNs of
  # policies created in this same apply (e.g. aws_iam_policy.*.arn), which
  # aren't known until apply. for_each requires its *keys* to be known at
  # plan time; the list length (and therefore the set of indexes) is, even
  # when individual ARN values are not.
  for_each   = { for idx, arn in var.policy_arns : tostring(idx) => arn }
  role       = aws_iam_role.irsa_role.name
  policy_arn = each.value
}
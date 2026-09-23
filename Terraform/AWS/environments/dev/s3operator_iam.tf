data "aws_caller_identity" "current" {}

# Permissions boundary attached to every app-irsa-* role the operator
# creates: caps what that role can ever do, regardless of the inline policy
# the operator's own PutRolePolicy call writes onto it. Kept in lockstep
# with the actual per-app policy ReconcileAppIRSA writes (S3 object access
# only) -- this is the ceiling, not the normal grant, so a bug or
# compromise in the operator's own code can never escalate an app-irsa-*
# role beyond plain S3 object access, no matter what policy JSON it tries
# to attach.
resource "aws_iam_policy" "app_irsa_boundary" {
  name        = "dev-app-irsa-boundary"
  description = "Permissions boundary for app-irsa-* roles created by forge-operator"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "MaxS3ObjectAccess"
        Effect = "Allow"
        Action = [
          "s3:ListBucket",
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
        ]
        Resource = [
          "arn:aws:s3:::*",
          "arn:aws:s3:::*/*",
        ]
      }
    ]
  })
}

resource "aws_iam_policy" "s3operator_permissions" {

  name        = "dev-app-operator-control-policy"
  description = "Allows operator to create S3 buckets and scope IAM roles for apps"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "S3BucketClaim"
        Effect = "Allow"
        Action = [
          # Bucket creation, and reading/first-writing the ownership tag --
          # these run before ownership can be confirmed one way or the
          # other (a brand new bucket has no tag yet; a foreign bucket's
          # tag has to be readable to correctly report it as not-owned), so
          # none of them can be gated on the tag already being present the
          # way S3BucketManageOwned below is.
          "s3:CreateBucket",
          "s3:PutBucketTagging",
          "s3:GetBucketTagging",
          "s3:ListBucket",
        ]
        # spec.storage.bucket accepts any user-supplied name
        Resource = "arn:aws:s3:::*"
      },
      {
        Sid    = "S3BucketManageOwned"
        Effect = "Allow"
        Action = [
          "s3:DeleteBucket",

          # Lifecycle Management
          "s3:PutLifecycleConfiguration",
          "s3:GetLifecycleConfiguration",
          "s3:DeleteBucketLifecycle",

          # Bucket Versioning
          "s3:PutBucketVersioning",
          "s3:GetBucketVersioning",
          "s3:ListBucketVersions",

          # Multipart upload cleanup (finalizer bucket teardown)
          "s3:ListMultipartUploads"
        ]
        Resource = "arn:aws:s3:::*"
        Condition = {
          # IAM-enforced backstop independent of the Go-level ownership
          # check (claimOrVerifyOwnership): these actions -- DeleteBucket
          # above all -- only ever apply to a bucket this operator has
          # already tagged as its own, so a bug or compromise in that Go
          # check can't delete or reconfigure a bucket it never claimed.
          # Doesn't cover S3ObjectManagement below: individual objects
          # aren't tagged by this operator (only the bucket/marker is), so
          # aws:ResourceTag can't be applied to them the same way.
          Null = {
            "aws:ResourceTag/forge-operator.ningendo7.github.io/owner-uid" = "false"
          }
        }
      },
      {
        Sid    = "S3ObjectManagement"
        Effect = "Allow"
        Action = [
          # Object-level cleanup during finalizer bucket teardown
          "s3:DeleteObject",
          "s3:DeleteObjectVersion",
          "s3:AbortMultipartUpload"
        ]
        Resource = "arn:aws:s3:::*/*"
      },
      {
        Sid    = "IAMRoleCreationForApps"
        Effect = "Allow"
        Action = [
          "iam:CreateRole"
        ]
        Resource = [
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/app-irsa-*",
        ]
        Condition = {
          # Every app-irsa-* role this operator creates must carry the
          # boundary above -- IAM rejects any CreateRole call for this
          # resource pattern that omits it or names a different boundary.
          StringEquals = {
            "iam:PermissionsBoundary" = aws_iam_policy.app_irsa_boundary.arn
          }
        }
      },
      {
        Sid    = "IAMRoleManagementForApps"
        Effect = "Allow"
        Action = [
          "iam:DeleteRole",
          "iam:GetRole",
          "iam:PutRolePolicy",
          "iam:DeleteRolePolicy",
          "iam:UpdateAssumeRolePolicy"
        ]
        Resource = [
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/app-irsa-*",
        ]
      }
    ]
  })
}

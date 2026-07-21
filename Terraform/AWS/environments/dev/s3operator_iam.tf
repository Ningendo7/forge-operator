data "aws_caller_identity" "current" {}

resource "aws_iam_policy" "s3operator_permissions" {

         name        = "dev-app-operator-control-policy"
         description = "Allows operator to create S3buckets and scoping IAM roles for apps"

         policy      = jsonencode({
                  Version = "2012-10-17"
                  Statement = [
                           {
                                    Sid      = "S3BucketManagement"
                                    Effect   = "Allow"
                                    Action   = [
                                             
                                             # Bucket Creation and Cleanup
                                             "s3:CreateBucket",
                                             "s3:DeleteBucket",
                                             "s3:PutBucketTagging",
                                             "s3:GetBucketTagging",

                                             # Lifecycle Management
                                             "s3:PutLifecycleConfiguration",
                                             "s3:GetLifecycleConfiguration",
                                             "s3:DeleteBucketLifecycle",

                                             # Bucket Versioning
                                             "s3:PutBucketVersioning",
                                             "s3:GetBucketVersioning",
                                             "s3:ListBucketVersions",

                                             # Bucket Policy Management
                                             "s3:PutBucketPolicy",
                                             "s3:GetBucketPolicy",

                                             "s3:ListBucket",
                                             "s3:GetObject",
                                             "s3:PutObject",
                                             "s3:DeleteObject"
                                    ]
                                    Resource = "arn:aws:s3:::app-data-*"
                           },
                           {
                                    Sid      = "S3ObjectManagement"
                                    Effect   = "Allow"
                                    Action   = [
                                             # Object-level Management
                                             "s3:PutObject",
                                             "s3:GetObject",
                                             "s3:DeleteObject",
                                             "s3:DeleteObjectVersion",
                                             "s3:ListObjectVersions"
                                    ]
                                    Resource = "arn:aws:s3:::app-data-*/*"
                           },
                           {
                                    Sid      = "IAMRoleManagementForApps"
                                    Effect   = "Allow"
                                    Action   = [
                                             "iam:CreateRole",
                                             "iam:DeleteRole",
                                             "iam:GetRole",
                                             "iam:TagRole",
                                             "iam:PutRolePolicy",
                                             "iam:DeleteRolePolicy",
                                             "iam:GetRolePolicy",
                                             "iam:PassRole"
                                    ]
                                    Resource = [ 
                                             "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/app-irsa-*",
                                    ]
                           }
                  ]
         })
}
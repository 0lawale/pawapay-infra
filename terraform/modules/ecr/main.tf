# -----------------------------------------------------------------------------
# ECR Module
#
# Creates an Elastic Container Registry repository for the ConfigMirror
# operator Docker image.
#
# SECURITY NOTES:
#   - Image scanning on push: every image is automatically scanned for
#     known CVEs (Common Vulnerabilities and Exposures) the moment it lands
#   - Encryption at rest using KMS
#   - Lifecycle policy: automatically removes untagged/old images to reduce
#     attack surface and storage costs
#   - Repository policy: only the EKS node role and CI/CD role can push/pull
#     — no wildcard access
# -----------------------------------------------------------------------------

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ---------------------------------------------------------------------------
# KMS Key for ECR encryption at rest
# ---------------------------------------------------------------------------
resource "aws_kms_key" "ecr" {
  description             = "KMS key for ECR encryption - ${var.project_name}-${var.environment}"
  deletion_window_in_days = 7
  enable_key_rotation     = true

  tags = {
    Name = "${var.project_name}-${var.environment}-ecr-kms"
  }
}

resource "aws_kms_alias" "ecr" {
  name          = "alias/${var.project_name}-${var.environment}-ecr"
  target_key_id = aws_kms_key.ecr.key_id
}

# ---------------------------------------------------------------------------
# ECR Repository
# ---------------------------------------------------------------------------
resource "aws_ecr_repository" "this" {
  name                 = "${var.project_name}-${var.environment}/configmirror-operator"
  image_tag_mutability = "IMMUTABLE" # SECURITY: tags cannot be overwritten; enforces image provenance

  # SECURITY: Scan every image on push for vulnerabilities
  image_scanning_configuration {
    scan_on_push = true
  }

  # SECURITY: Encrypt images at rest with our KMS key
  encryption_configuration {
    encryption_type = "KMS"
    kms_key         = aws_kms_key.ecr.arn
  }

  tags = {
    Name = "${var.project_name}-${var.environment}-configmirror-operator"
  }
}

# ---------------------------------------------------------------------------
# Lifecycle Policy
# SECURITY + COST: Keep only the last 10 tagged releases and remove all
# untagged images after 1 day. Stale images = stale vulnerabilities.
# ---------------------------------------------------------------------------
resource "aws_ecr_lifecycle_policy" "this" {
  repository = aws_ecr_repository.this.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Remove untagged images after 1 day"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only last 10 tagged releases"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["v"]
          countType     = "imageCountMoreThan"
          countNumber   = 10
        }
        action = { type = "expire" }
      }
    ]
  })
}

# ---------------------------------------------------------------------------
# Repository Policy — Least Privilege
# SECURITY: Only explicitly named roles can interact with this repo.
#   - EKS node role : pull only (nodes need to download the image)
#   - CI/CD role    : push + pull (GitHub Actions pushes new images)
# ---------------------------------------------------------------------------
resource "aws_ecr_repository_policy" "this" {
  repository = aws_ecr_repository.this.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowEKSNodePull"
        Effect = "Allow"
        Principal = {
          AWS = var.eks_node_role_arn
        }
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability"
        ]
      },
      {
        Sid    = "AllowCICDPushPull"
        Effect = "Allow"
        Principal = {
          AWS = aws_iam_role.cicd.arn
        }
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability",
          "ecr:PutImage",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:DescribeRepositories",
          "ecr:GetRepositoryPolicy",
          "ecr:ListImages"
        ]
      }
    ]
  })
}

# ---------------------------------------------------------------------------
# IAM Role for CI/CD (GitHub Actions) — OIDC-based, no long-lived keys
#
# SECURITY: Instead of storing AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
# as GitHub secrets, GitHub Actions assumes this role via OIDC federation.
# This means there are ZERO long-lived credentials stored anywhere.
# ---------------------------------------------------------------------------
resource "aws_iam_role" "cicd" {
  name = "${var.project_name}-${var.environment}-cicd-ecr-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/token.actions.githubusercontent.com"
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringLike = {
          # SECURITY: Scope to only YOUR repo — change this to your GitHub org/repo
          "token.actions.githubusercontent.com:sub" = "repo:${var.github_org}/${var.github_repo}:*"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "cicd_ecr" {
  name = "cicd-ecr-push-policy"
  role = aws_iam_role.cicd.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = "ecr:GetAuthorizationToken"
        # GetAuthorizationToken is account-level, not resource-level
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability",
          "ecr:PutImage",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload"
        ]
        # Scoped to only this specific repository
        Resource = aws_ecr_repository.this.arn
      }
    ]
  })
}

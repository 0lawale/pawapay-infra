# -----------------------------------------------------------------------------
# Root Terragrunt configuration for the "dev" environment.
# All child terragrunt.hcl files include this via find_in_parent_folders().
#
# Responsibilities:
#   - Configure the S3 remote state backend (with DynamoDB locking)
#   - Inject common AWS provider settings into every module
#   - Expose shared local variables (region, env name, project name)
# -----------------------------------------------------------------------------

locals {
  aws_region   = "us-east-1"
  environment  = "dev"
  project_name = "pawapay"
}

# ---------------------------------------------------------------------------
# Remote State: S3 bucket + DynamoDB lock table
# SECURITY: The S3 bucket must have:
#   - versioning enabled (recover from accidental state corruption)
#   - server-side encryption (AES-256 or aws:kms)
#   - public access fully blocked
#   - access logging to a separate audit bucket
# The DynamoDB table prevents concurrent Terraform runs from corrupting state.
# ---------------------------------------------------------------------------
remote_state {
  backend = "s3"

  config = {
    bucket         = "${local.project_name}-terraform-state-${local.environment}"
    key            = "${path_relative_to_include()}/terraform.tfstate"
    region         = local.aws_region
    encrypt        = true                                   # AES-256 server-side encryption
    dynamodb_table = "${local.project_name}-terraform-locks"
  }

  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
}

# ---------------------------------------------------------------------------
# AWS Provider — injected into every child module automatically.
# default_tags ensures every single AWS resource is tagged consistently
# for cost tracking, ownership, and compliance.
# ---------------------------------------------------------------------------
generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite_terragrunt"

  contents = <<EOF
provider "aws" {
  region = "${local.aws_region}"

  default_tags {
    tags = {
      Environment = "${local.environment}"
      Project     = "${local.project_name}"
      ManagedBy   = "Terraform"
    }
  }
}

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
EOF
}

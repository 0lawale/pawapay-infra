# GitHub Actions — Required Secrets Setup

Go to: **GitHub repo → Settings → Secrets and variables → Actions → New repository secret**

Add each of the following secrets:

---

## Secrets to Configure

| Secret Name | Value | Where to get it |
|---|---|---|
| `AWS_CICD_ROLE_ARN` | ARN of the CI/CD IAM role | `terraform -chdir=terraform/environments/dev/ecr output -raw cicd_role_arn` |
| `ECR_REPOSITORY_URL` | Full ECR repo URL | `terraform -chdir=terraform/environments/dev/ecr output -raw repository_url` |
| `OPERATOR_IRSA_ROLE_ARN` | ARN of the operator IRSA role | `terraform -chdir=terraform/environments/dev/eks output -raw operator_irsa_role_arn` |
| `SSM_USERNAME_PATH` | SSM path for DB username | `terraform -chdir=terraform/environments/dev/rds output -raw ssm_username_path` |
| `SSM_PASSWORD_PATH` | SSM path for DB password | `terraform -chdir=terraform/environments/dev/rds output -raw ssm_password_path` |
| `SSM_ENDPOINT_PATH` | SSM path for DB endpoint | `terraform -chdir=terraform/environments/dev/rds output -raw ssm_endpoint_path` |

---

## Why OIDC Instead of Access Keys?

Traditional CI/CD stores `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` as
GitHub Secrets. These are long-lived credentials that:
- Never expire automatically
- Can be leaked in logs if not masked carefully
- Must be rotated manually

With OIDC federation:
1. GitHub Actions requests a short-lived OIDC token from GitHub's identity provider
2. AWS STS exchanges that token for temporary credentials (expire in 1 hour)
3. Zero secrets stored — nothing to leak, nothing to rotate

The ECR Terraform module already created the OIDC trust relationship.
You just need to set `AWS_CICD_ROLE_ARN` above.

---

## One-time: Enable GitHub OIDC for Your AWS Account

If this is the first time using GitHub OIDC with this AWS account:

```bash
# Add GitHub's OIDC provider to AWS IAM (one-time per account)
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

The ECR module's `aws_iam_role.cicd` already references this provider.

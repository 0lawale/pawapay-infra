output "repository_url" {
  description = "Full URL of the ECR repository (used in docker push commands and Helm values)"
  value       = aws_ecr_repository.this.repository_url
}

output "repository_arn" {
  description = "ARN of the ECR repository"
  value       = aws_ecr_repository.this.arn
}

output "cicd_role_arn" {
  description = "ARN of the CI/CD IAM role (configured in GitHub Actions as the assumed role)"
  value       = aws_iam_role.cicd.arn
}

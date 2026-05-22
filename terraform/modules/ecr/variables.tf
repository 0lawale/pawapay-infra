variable "project_name" {
  description = "Name of the project"
  type        = string
}

variable "environment" {
  description = "Deployment environment"
  type        = string
}

variable "eks_node_role_arn" {
  description = "IAM role ARN of the EKS node group — granted ECR pull access"
  type        = string
}

variable "github_org" {
  description = "GitHub organisation name (used to scope the OIDC trust policy)"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name (used to scope the OIDC trust policy)"
  type        = string
}

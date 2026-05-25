output "cluster_name" {
  description = "Name of the EKS cluster"
  value       = aws_eks_cluster.this.name
}

output "cluster_endpoint" {
  description = "API server endpoint of the EKS cluster"
  value       = aws_eks_cluster.this.endpoint
}

output "cluster_ca_certificate" {
  description = "Base64-encoded certificate authority data for the cluster"
  value       = aws_eks_cluster.this.certificate_authority[0].data
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC provider (used to create IRSA roles for pods)"
  value       = aws_iam_openid_connect_provider.eks.arn
}

output "oidc_provider_url" {
  description = "URL of the OIDC provider"
  value       = aws_iam_openid_connect_provider.eks.url
}

output "node_security_group_id" {
  description = "Security group ID of the worker nodes"
  value       = aws_security_group.node_group.id
}

output "node_role_arn" {
  description = "IAM role ARN of the EKS node group"
  value       = aws_iam_role.node_group.arn
}

output "operator_irsa_role_arn" {
  description = "ARN of the IRSA role for the ConfigMirror operator pod"
  value       = aws_iam_role.operator_irsa.arn
}

output "cluster_security_group_id" {
  description = "EKS-managed cluster security group attached to all pods"
  value       = aws_eks_cluster.this.vpc_config[0].cluster_security_group_id
}

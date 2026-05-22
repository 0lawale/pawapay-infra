output "db_endpoint" {
  description = "RDS connection endpoint (host:port)"
  value       = aws_db_instance.this.endpoint
}

output "db_name" {
  description = "Name of the database"
  value       = aws_db_instance.this.db_name
}

output "db_port" {
  description = "Database port"
  value       = aws_db_instance.this.port
}

output "ssm_password_path" {
  description = "SSM Parameter Store path for the DB password"
  value       = aws_ssm_parameter.db_password.name
}

output "ssm_username_path" {
  description = "SSM Parameter Store path for the DB username"
  value       = aws_ssm_parameter.db_username.name
}

output "ssm_endpoint_path" {
  description = "SSM Parameter Store path for the DB endpoint"
  value       = aws_ssm_parameter.db_endpoint.name
}

output "operator_iam_policy_arn" {
  description = "IAM policy ARN to attach to the operator's IRSA role"
  value       = aws_iam_policy.operator_rds_access.arn
}

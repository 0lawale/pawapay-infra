variable "project_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "vpc_id" {
  description = "VPC ID where RDS will be created"
  type        = string
}

variable "db_subnet_group_name" {
  description = "Name of the DB subnet group (from VPC module output)"
  type        = string
}

variable "eks_node_security_group_id" {
  description = "Security group ID of EKS nodes — the only source allowed to reach RDS"
  type        = string
}

variable "db_name" {
  description = "Name of the initial database"
  type        = string
  default     = "configmirror"
}

variable "db_username" {
  description = "Master username for the RDS instance"
  type        = string
  default     = "configmirror_admin"
}

variable "db_instance_class" {
  description = "RDS instance type"
  type        = string
  default     = "db.t3.micro"
}

variable "allocated_storage" {
  description = "Allocated storage in GB"
  type        = number
  default     = 20
}

variable "multi_az" {
  description = "Enable Multi-AZ for high availability. Set true for production."
  type        = bool
  default     = false
}

variable "deletion_protection" {
  description = "Prevent accidental deletion. Set true for production."
  type        = bool
  default     = false
}

variable "skip_final_snapshot" {
  description = "Skip final snapshot on destroy. Set false for production."
  type        = bool
  default     = true
}

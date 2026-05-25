# environments/dev/rds/terragrunt.hcl
# Deploys the RDS module for the dev environment.
# Depends on VPC and EKS — needs subnet group and node security group.

include "root" {
  path = find_in_parent_folders()
}

terraform {
  source = "../../../modules/rds"
}

dependency "vpc" {
  config_path = "../vpc"

  mock_outputs_allowed_terraform_commands = ["validate", "plan"]
  mock_outputs = {
    vpc_id               = "vpc-00000000"
    db_subnet_group_name = "mock-db-subnet-group"
  }
}

dependency "eks" {
  config_path = "../eks"

  mock_outputs_allowed_terraform_commands = ["validate", "plan"]
  mock_outputs = {
    node_security_group_id = "sg-00000000"
    cluster_security_group_id = "sg-00000001"
  }
}

inputs = {
  project_name = "pawapay"
  environment  = "dev"

  vpc_id                     = dependency.vpc.outputs.vpc_id
  db_subnet_group_name       = dependency.vpc.outputs.db_subnet_group_name
  eks_node_security_group_id = dependency.eks.outputs.node_security_group_id
  eks_cluster_security_group_id = dependency.eks.outputs.cluster_security_group_id

  db_name           = "configmirror"
  db_username       = "configmirror_admin"
  db_instance_class = "db.t3.micro"
  allocated_storage = 20

  # Dev settings — flip these for production
  multi_az            = false
  deletion_protection = false
  skip_final_snapshot = true
}

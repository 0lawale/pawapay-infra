# environments/dev/eks/terragrunt.hcl
# Deploys the EKS module for the dev environment.
# Depends on VPC — reads vpc_id and subnet IDs from its remote state.

include "root" {
  path = find_in_parent_folders()
}

terraform {
  source = "../../../modules/eks"
}

# Pull outputs from the VPC module's state — no copy-pasting IDs
dependency "vpc" {
  config_path = "../vpc"

  # Allows plan to run before vpc is applied (returns dummy values)
  mock_outputs_allowed_terraform_commands = ["validate", "plan"]
  mock_outputs = {
    vpc_id             = "vpc-00000000"
    private_subnet_ids = ["subnet-00000001", "subnet-00000002"]
    public_subnet_ids  = ["subnet-00000003", "subnet-00000004"]
  }
}

inputs = {
  project_name = "pawapay"
  environment  = "dev"

  vpc_id             = dependency.vpc.outputs.vpc_id
  private_subnet_ids = dependency.vpc.outputs.private_subnet_ids
  public_subnet_ids  = dependency.vpc.outputs.public_subnet_ids

  kubernetes_version     = "1.29"
  node_instance_types    = ["t3.medium"]
  node_desired_size      = 2
  node_min_size          = 1
  node_max_size          = 4
  enable_public_endpoint = true   # Set false for production
}

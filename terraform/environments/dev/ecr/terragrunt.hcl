# environments/dev/ecr/terragrunt.hcl
# Deploys the ECR module for the dev environment.
# Depends on EKS — needs the node role ARN for the repo policy.

include "root" {
  path = find_in_parent_folders()
}

terraform {
  source = "../../../modules/ecr"
}

dependency "eks" {
  config_path = "../eks"

  mock_outputs_allowed_terraform_commands = ["validate", "plan"]
  mock_outputs = {
    node_role_arn = "arn:aws:iam::123456789012:role/mock-node-role"
  }
}

inputs = {
  project_name = "pawapay"
  environment  = "dev"

  eks_node_role_arn = dependency.eks.outputs.node_role_arn

  # IMPORTANT: Replace with your actual GitHub org and repo name
  github_org  = "0lawale"
  github_repo = "pawapay-infra"
}

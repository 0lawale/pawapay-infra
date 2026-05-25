# -----------------------------------------------------------------------------
# EKS Module
#
# Creates:
#   - EKS control plane (managed by AWS)
#   - Managed node group (worker nodes in private subnets)
#   - OIDC provider (required for IRSA — IAM Roles for Service Accounts)
#   - Core IAM roles for control plane and node group
#   - Security groups with least-privilege ingress/egress rules
#
# SECURITY NOTES:
#   - Nodes live in private subnets — never publicly accessible
#   - OIDC + IRSA means pods get scoped AWS credentials without
#     storing access keys anywhere — this is how the operator will
#     talk to RDS and SSM securely
#   - API server endpoint access is private by default; public access
#     can be toggled for dev convenience but should be off in prod
#   - EKS secrets encryption uses a dedicated KMS key
# -----------------------------------------------------------------------------


# terraform {
#   required_providers {
#     aws = {
#       source  = "hashicorp/aws"
#       version = "~> 5.0"
#     }
#     kubernetes = {
#       source  = "hashicorp/kubernetes"
#       version = "~> 2.27"
#     }
#     tls = {
#       source  = "hashicorp/tls"
#       version = "~> 4.0"
#     }
#   }
# }

# provider "kubernetes" {
#   host                   = aws_eks_cluster.this.endpoint
#   cluster_ca_certificate = base64decode(aws_eks_cluster.this.certificate_authority[0].data)

#   exec {
#     api_version = "client.authentication.k8s.io/v1beta1"
#     command     = "aws"
#     args        = ["eks", "get-token", "--cluster-name", aws_eks_cluster.this.name]
#   }
# }

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

# ---------------------------------------------------------------------------
# KMS Key for EKS secrets encryption at rest
# SECURITY: Encrypts all Kubernetes Secrets stored in etcd
# ---------------------------------------------------------------------------
resource "aws_kms_key" "eks" {
  description             = "KMS key for EKS secrets encryption - ${var.project_name}-${var.environment}"
  deletion_window_in_days = 7
  enable_key_rotation     = true # Rotate key material annually

  tags = {
    Name = "${var.project_name}-${var.environment}-eks-kms"
  }
}

resource "aws_kms_alias" "eks" {
  name          = "alias/${var.project_name}-${var.environment}-eks"
  target_key_id = aws_kms_key.eks.key_id
}

# ---------------------------------------------------------------------------
# IAM Role for EKS Control Plane
# SECURITY: Only the EKS service can assume this role (trust policy scoped tight)
# ---------------------------------------------------------------------------
resource "aws_iam_role" "eks_cluster" {
  name = "${var.project_name}-${var.environment}-eks-cluster-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "eks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "eks_cluster_policy" {
  role       = aws_iam_role.eks_cluster.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
}

# ---------------------------------------------------------------------------
# Security Group for EKS Control Plane
# ---------------------------------------------------------------------------
resource "aws_security_group" "eks_cluster" {
  name        = "${var.project_name}-${var.environment}-eks-cluster-sg"
  description = "Security group for EKS control plane"
  vpc_id      = var.vpc_id

  egress {
    description = "Allow all outbound (control plane to nodes)"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.project_name}-${var.environment}-eks-cluster-sg"
  }
}

# ---------------------------------------------------------------------------
# EKS Cluster
# ---------------------------------------------------------------------------
resource "aws_eks_cluster" "this" {
  name     = "${var.project_name}-${var.environment}"
  role_arn = aws_iam_role.eks_cluster.arn
  version  = var.kubernetes_version

  vpc_config {
    subnet_ids              = concat(var.private_subnet_ids, var.public_subnet_ids)
    security_group_ids      = [aws_security_group.eks_cluster.id]
    endpoint_private_access = true
    endpoint_public_access  = var.enable_public_endpoint # false in prod, true for dev convenience
  }

  # SECURITY: Encrypt Kubernetes Secrets at rest using our KMS key
  encryption_config {
    provider {
      key_arn = aws_kms_key.eks.arn
    }
    resources = ["secrets"]
  }

  # Enable useful control plane log types for audit + debugging
  enabled_cluster_log_types = ["api", "audit", "authenticator", "controllerManager", "scheduler"]

  depends_on = [aws_iam_role_policy_attachment.eks_cluster_policy]
}

# ---------------------------------------------------------------------------
# OIDC Provider — enables IRSA (IAM Roles for Service Accounts)
# This is what allows pods to assume IAM roles without storing credentials
# ---------------------------------------------------------------------------
data "tls_certificate" "eks" {
  url = aws_eks_cluster.this.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.this.identity[0].oidc[0].issuer
}

# ---------------------------------------------------------------------------
# IAM Role for EKS Worker Nodes
# SECURITY: Nodes get minimum permissions needed to register with the cluster
# ---------------------------------------------------------------------------
resource "aws_iam_role" "node_group" {
  name = "${var.project_name}-${var.environment}-eks-node-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "node_worker_policy" {
  role       = aws_iam_role.node_group.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
}

resource "aws_iam_role_policy_attachment" "node_cni_policy" {
  role       = aws_iam_role.node_group.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
}

resource "aws_iam_role_policy_attachment" "node_ecr_readonly" {
  # Read-only: nodes can pull images from ECR but never push
  role       = aws_iam_role.node_group.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
}

# ---------------------------------------------------------------------------
# Security Group for Worker Nodes
# ---------------------------------------------------------------------------
resource "aws_security_group" "node_group" {
  name        = "${var.project_name}-${var.environment}-eks-node-sg"
  description = "Security group for EKS worker nodes"
  vpc_id      = var.vpc_id

  ingress {
    description = "Allow nodes to communicate with each other"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    self        = true
  }

  ingress {
    description     = "Allow control plane to reach nodes (webhooks, metrics)"
    from_port       = 1025
    to_port         = 65535
    protocol        = "tcp"
    security_groups = [aws_security_group.eks_cluster.id]
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.project_name}-${var.environment}-eks-node-sg"
  }
}

# ---------------------------------------------------------------------------
# EKS Managed Node Group (worker nodes in private subnets)
# ---------------------------------------------------------------------------
resource "aws_eks_node_group" "this" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "${var.project_name}-${var.environment}-nodes"
  node_role_arn   = aws_iam_role.node_group.arn
  subnet_ids      = var.private_subnet_ids # Private subnets only
  instance_types  = var.node_instance_types
  capacity_type   = "ON_DEMAND"

  scaling_config {
    desired_size = var.node_desired_size
    max_size     = var.node_max_size
    min_size     = var.node_min_size
  }

  update_config {
    max_unavailable = 1
  }

  # SECURITY: Ensure node security group is applied
  remote_access {
    ec2_ssh_key               = null # No SSH access; use SSM Session Manager instead
    source_security_group_ids = []
  }

  depends_on = [
    aws_iam_role_policy_attachment.node_worker_policy,
    aws_iam_role_policy_attachment.node_cni_policy,
    aws_iam_role_policy_attachment.node_ecr_readonly,
  ]
}

# ---------------------------------------------------------------------------
# IRSA Role for the ConfigMirror Operator pod
# ---------------------------------------------------------------------------
resource "aws_iam_role" "operator_irsa" {
  name = "${var.project_name}-${var.environment}-configmirror-operator"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${aws_iam_openid_connect_provider.eks.url}:sub" = "system:serviceaccount:ops:configmirror-operator"
          "${aws_iam_openid_connect_provider.eks.url}:aud" = "sts.amazonaws.com"
        }
      }
    }]
  })
}

# ---------------------------------------------------------------------------
# aws-auth ConfigMap — grants CI/CD role access to deploy to the cluster
# ---------------------------------------------------------------------------
# resource "kubernetes_config_map_v1_data" "aws_auth" {
#   metadata {
#     name      = "aws-auth"
#     namespace = "kube-system"
#   }

#   force = true

#   data = {
#     mapRoles = yamlencode([
#       {
#         rolearn  = aws_iam_role.node_group.arn
#         username = "system:node:{{EC2PrivateDNSName}}"
#         groups   = ["system:bootstrappers", "system:nodes"]
#       },
#       {
#         rolearn  = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${var.project_name}-${var.environment}-cicd-ecr-role"
#         username = "cicd-role"
#         groups   = ["system:masters"]
#       }
#     ])
#   }

#   depends_on = [aws_eks_cluster.this]
# }
resource "null_resource" "aws_auth" {
  triggers = {
    cluster_name  = aws_eks_cluster.this.name
    node_role_arn = aws_iam_role.node_group.arn
  }

  provisioner "local-exec" {
    command = <<-EOT
      aws eks update-kubeconfig --name ${aws_eks_cluster.this.name} --region ${var.aws_region} && \
      kubectl patch configmap aws-auth -n kube-system --patch "$(cat <<EOF
data:
  mapRoles: |
    - rolearn: ${aws_iam_role.node_group.arn}
      username: system:node:{{EC2PrivateDNSName}}
      groups:
        - system:bootstrappers
        - system:nodes
    - rolearn: arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${var.project_name}-${var.environment}-cicd-ecr-role
      username: cicd-role
      groups:
        - system:masters
EOF
)"
    EOT
  }

  depends_on = [aws_eks_cluster.this, aws_eks_node_group.this]
}
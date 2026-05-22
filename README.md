# pawaPay Infrastructure — ConfigMirror Operator

End-to-end infrastructure and Kubernetes operator for mirroring ConfigMaps across namespaces with RDS persistence.

---

## Architecture

```
┌─────────────────────────────────── AWS us-east-1 ──────────────────────────────────┐
│                                                                                      │
│   ┌──────── VPC 10.0.0.0/16 ────────────────────────────────────────────────────┐  │
│   │                                                                               │  │
│   │  Public Subnets (10.0.1-2.0/24)     ← NAT Gateways, Load Balancers only    │  │
│   │                                                                               │  │
│   │  Private Subnets (10.0.10-11.0/24)  ← EKS Worker Nodes                     │  │
│   │    └─ ConfigMirror Operator Pod                                              │  │
│   │         └─ (IRSA) fetches creds from SSM → connects to RDS                  │  │
│   │                                                                               │  │
│   │  Database Subnets (10.0.20-21.0/24) ← RDS PostgreSQL (no internet route)   │  │
│   │                                                                               │  │
│   └───────────────────────────────────────────────────────────────────────────── ┘  │
│                                                                                      │
│   ECR Repository  ← GitHub Actions pushes operator image here                       │
│   SSM Parameter Store ← DB credentials stored here (KMS encrypted)                  │
└──────────────────────────────────────────────────────────────────────────────────── ┘
```

---

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Terraform | >= 1.5.0 | https://developer.hashicorp.com/terraform/install |
| Terragrunt | >= 0.55.0 | https://terragrunt.gruntwork.io/docs/getting-started/install |
| AWS CLI | >= 2.x | https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html |
| kubectl | >= 1.29 | https://kubernetes.io/docs/tasks/tools |
| Helm | >= 3.x | https://helm.sh/docs/intro/install |
| Go | >= 1.21 | https://go.dev/doc/install |
| Docker | >= 24.x | https://docs.docker.com/engine/install |

---

## Repository Structure

```
pawapay-infra/
├── terraform/
│   ├── modules/
│   │   ├── vpc/          # VPC, subnets, NAT, flow logs
│   │   ├── eks/          # EKS cluster, node group, OIDC/IRSA
│   │   ├── ecr/          # ECR repository, lifecycle, CI/CD role
│   │   └── rds/          # PostgreSQL RDS, SSM secrets, IRSA policy
│   └── environments/
│       └── dev/
│           ├── terragrunt.hcl   # Root: S3 backend + provider
│           ├── vpc/
│           ├── eks/
│           ├── ecr/
│           └── rds/
├── operator/             # ConfigMirror operator (Go + Kubebuilder)
├── helm/                 # Helm chart for operator deployment
├── .github/workflows/    # CI/CD GitHub Actions
└── README.md
```

---

## Step 1 — Bootstrap Remote State

Before running Terragrunt, manually create the S3 bucket and DynamoDB table
for remote state. This is a one-time step.

```bash
# Set your variables
export AWS_REGION="us-east-1"
export PROJECT="pawapay"
export ENV="dev"
export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# Create S3 bucket for state
aws s3api create-bucket \
  --bucket "${PROJECT}-terraform-state-${ENV}" \
  --region "${AWS_REGION}"

# Enable versioning
aws s3api put-bucket-versioning \
  --bucket "${PROJECT}-terraform-state-${ENV}" \
  --versioning-configuration Status=Enabled

# Enable encryption
aws s3api put-bucket-encryption \
  --bucket "${PROJECT}-terraform-state-${ENV}" \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {"SSEAlgorithm": "AES256"}
    }]
  }'

# Block all public access (SECURITY)
aws s3api put-public-access-block \
  --bucket "${PROJECT}-terraform-state-${ENV}" \
  --public-access-block-configuration \
    "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"

# Create DynamoDB table for state locking
aws dynamodb create-table \
  --table-name "${PROJECT}-terraform-locks" \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST \
  --region "${AWS_REGION}"
```

---

## Step 2 — Configure AWS CLI

```bash
aws configure
# AWS Access Key ID: <your key>
# AWS Secret Access Key: <your secret>
# Default region: us-east-1
# Default output format: json

# Verify
aws sts get-caller-identity
```

---

## Step 3 — Update Variables

Before applying, update these values in the Terragrunt configs:

**`terraform/environments/dev/ecr/terragrunt.hcl`**
```hcl
github_org  = "your-actual-github-org"
github_repo = "your-actual-repo-name"
```

---

## Step 4 — Deploy Infrastructure

Deploy modules in order (each depends on the previous):

```bash
cd terraform/environments/dev

# 1. VPC first — everything else depends on it
cd vpc && terragrunt apply && cd ..

# 2. EKS — needs VPC subnets
cd eks && terragrunt apply && cd ..

# 3. ECR — needs EKS node role ARN
cd ecr && terragrunt apply && cd ..

# 4. RDS — needs VPC subnet group and EKS node security group
cd rds && terragrunt apply && cd ..
```

Or deploy everything at once (Terragrunt handles dependency order):

```bash
cd terraform/environments/dev
terragrunt run-all apply
```

---

## Step 5 — Configure kubectl

```bash
aws eks update-kubeconfig \
  --name pawapay-dev \
  --region us-east-1

# Verify nodes are Ready
kubectl get nodes
```

---

## Step 6 — Deploy the Operator

```bash
# Install the Helm chart (installs CRD + operator deployment)
helm install configmirror ./helm/configmirror-operator \
  --namespace ops \
  --create-namespace \
  --set image.repository=$(terraform -chdir=terraform/environments/dev/ecr output -raw repository_url) \
  --set image.tag=latest

# Verify the operator is running
kubectl get pods -n ops
```

---

## Step 7 — Create a ConfigMirror Resource

```bash
kubectl apply -f - <<EOF
apiVersion: ops.pawapay.io/v1alpha1
kind: ConfigMirror
metadata:
  name: app-config-mirror
  namespace: ops
spec:
  sourceNamespace: app-source
  targetNamespaces:
    - app-staging
    - app-prod
  selector:
    matchLabels:
      app: myservice
EOF

# Watch the operator mirror ConfigMaps
kubectl get configmaps -n app-staging
kubectl get configmaps -n app-prod
```

---

## Teardown

```bash
# Remove operator
helm uninstall configmirror -n ops

# Destroy infrastructure (reverse order)
cd terraform/environments/dev
terragrunt run-all destroy
```

---

## Assumptions

1. **Single environment** (`dev`) is implemented. The module structure supports `staging` and `prod` by adding new environment folders.
2. **Single region** (`us-east-1`). Multi-region would require additional state buckets and provider aliases.
3. **Public EKS API endpoint** is enabled in dev for ease of access. This should be `false` in production with VPN/bastion access.
4. **`db.t3.micro`** is used for dev cost efficiency. Production should use `db.t3.small` or larger with `multi_az = true`.
5. **No SSH access** to EKS nodes — AWS SSM Session Manager is the intended access method for debugging.
6. **GitHub OIDC** is used for CI/CD authentication — no long-lived AWS access keys are stored in GitHub.

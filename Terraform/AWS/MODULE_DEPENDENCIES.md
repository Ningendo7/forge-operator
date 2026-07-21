# Terraform Module Dependency Map

## Module Dependency Flow

```
VPC Module (Independent)
├── Creates: VPC, Public/Private Subnets, NAT Gateways, IGW, Route Tables
├── Outputs: vpc_id, public_subnet_ids, private_subnet_ids, vpc_cidr_block
└── Variables: env, vpc_cidr, az_network_config, enable_single_nat_gateway, tags

    ↓ (provides vpc_id)

Networking Module (depends on VPC)
├── Creates: Security Groups (nodes, VPC endpoints)
├── Inputs: vpc_id, private_subnet_ids, env, tags
├── Outputs: node_security_group_id
└── Note: Security groups for cluster-internal communication and VPC endpoint access

    ↓ (provides node_security_group_id)

IAM Module (Independent)
├── Creates: Cluster IAM role, Node IAM role
├── Outputs: cluster_role_arn, node_role_arn
├── Variables: env, cluster_name, tags
└── Note: Policies for EKS control plane and worker node permissions

    ↓ (provides cluster_role_arn, node_role_arn)

EKS Module (depends on VPC, Networking, IAM)
├── Creates: EKS cluster, Node group, OIDC provider, Add-ons (VPC CNI, CoreDNS, kube-proxy)
├── Inputs:
│   ├── From VPC: private_subnet_ids
│   ├── From Networking: cluster_security_group_id
│   ├── From IAM: cluster_role_arn, node_role_arn
│   ├── From IRSA (optional): vpc_cni_role_arn
│   └── Configuration: cluster_version, node sizes, capacity type, tags
├── Outputs: cluster_name, cluster_endpoint, cluster_certificate_authority_data, oidc_provider_arn, oidc_provider_url
└── Note: Manages cluster lifecycle, add-ons, and node autoscaling

    ↓ (provides oidc_provider_arn, oidc_provider_url)

IRSA Module (depends on EKS) [OPTIONAL]
├── Creates: ServiceAccount IAM roles for workloads (e.g., VPC CNI, external-dns, cert-manager)
├── Inputs: env, cluster_name, oidc_provider_arn, oidc_provider_url, tags
├── Outputs: vpc_cni_role_arn, and other service account role ARNs
└── Note: Currently EMPTY - needs implementation for workload identity

    ↓ (provides OIDC-bound IAM roles)

Monitoring Module (depends on EKS) [OPTIONAL]
├── Creates: CloudWatch log groups, SNS topics, alarm rules
├── Inputs: env, cluster_name, cluster_endpoint, oidc_provider_arn, oidc_provider_url, tags
├── Outputs: log_group_name, sns_topic_arn, alarm_arns
└── Note: Currently EMPTY - needs implementation for observability
```

## Module Connection Reference Table

| Source Module | Output | → | Target Module | Input Parameter |
|---------------|--------|---|----------------|-----------------|
| VPC | vpc_id | → | Networking | vpc_id |
| VPC | private_subnet_ids | → | Networking | private_subnet_ids |
| VPC | private_subnet_ids | → | EKS | private_subnet_ids |
| Networking | node_security_group_id | → | EKS | cluster_security_group_id |
| IAM | cluster_role_arn | → | EKS | cluster_role_arn |
| IAM | node_role_arn | → | EKS | node_role_arn |
| EKS | oidc_provider_arn | → | IRSA | oidc_provider_arn |
| EKS | oidc_provider_url | → | IRSA | oidc_provider_url |
| EKS | oidc_provider_arn | → | Monitoring | oidc_provider_arn |
| EKS | cluster_endpoint | → | Monitoring | cluster_endpoint |
| IRSA | vpc_cni_role_arn | → | EKS | vpc_cni_role_arn |

## How Terraform Knows Which is Which

Terraform uses **explicit variable passing** via `module.<name>.<output>` to wire dependencies:

```hcl
# Example from main.tf:
module "vpc" {
  source = "../../modules/vpc"
  # VPC creates subnets independently
}

module "networking" {
  source = "../../modules/networking"
  vpc_id = module.vpc.vpc_id  # ← "Which is which" happens here
  # Networking KNOWS it's using VPC's output
}

module "eks" {
  source = "../../modules/eks"
  private_subnet_ids = module.vpc.private_subnet_ids  # ← Clear reference to VPC
  cluster_security_group_id = module.networking.node_security_group_id  # ← Clear reference to Networking
  cluster_role_arn = module.iam.cluster_role_arn  # ← Clear reference to IAM
}
```

**Key Points:**
1. Each module has **explicit input variables** (see `variables.tf` in each module)
2. Root composition (`main.tf`) passes module outputs to module inputs using `module.name.output` syntax
3. Terraform builds a **dependency graph** automatically based on these connections
4. If Module A output → Module B input, Terraform ensures A deploys before B
5. No "magic" — dependencies are explicit and traceable

## Environment Structure

```
AWS/
├── modules/
│   ├── vpc/           → VPC infrastructure (subnets, NAT, etc.)
│   ├── networking/    → Security groups, VPC endpoints
│   ├── iam/           → IAM roles for cluster and nodes
│   ├── eks/           → EKS cluster, node group, add-ons
│   ├── irsa/          → Service account roles (EMPTY - TODO)
│   └── monitoring/    → CloudWatch monitoring (EMPTY - TODO)
│
└── environments/
    ├── dev/
    │   ├── main.tf              ← ROOT COMPOSITION (orchestrates all modules)
    │   ├── variables.tf         ← Environment-specific defaults
    │   ├── outputs.tf           ← Values to display after deployment
    │   ├── providers.tf         ← AWS provider config
    │   └── versions.tf          ← Terraform version lock
    │
    └── prod/
        └── (same structure, different variables)
```

## Deployment Order

Terraform automatically determines order based on dependencies:

1. **VPC** (no dependencies) — Creates first
2. **Networking** (waits for VPC) — Creates second
3. **IAM** (no dependencies) — Creates in parallel with VPC/Networking
4. **EKS** (waits for VPC, Networking, IAM) — Creates fourth
5. **IRSA** (waits for EKS) — Optional, creates fifth if enabled
6. **Monitoring** (waits for EKS) — Optional, creates last if enabled

You can verify this order with: `terraform graph | grep -E "module\." | head -20`

## Usage Example

```bash
cd AWS/environments/dev

# Initialize Terraform (downloads providers, modules)
terraform init

# View planned changes (shows which resources will be created)
terraform plan

# Apply (creates all resources in dependency order)
terraform apply

# Get output values
terraform output cluster_name
terraform output cluster_endpoint
terraform output configure_kubectl
```

## Troubleshooting Module Connections

If Terraform complains about a missing variable:
1. Check which module's `variables.tf` defines it
2. Verify the root `main.tf` passes the correct module output
3. Example error: `Error: Missing required argument "vpc_id"`
   → Solution: Ensure `main.tf` passes `vpc_id = module.vpc.vpc_id` to the Networking module

## Next Steps

1. **Implement IRSA module** — Handle VPC CNI and app workload permissions
2. **Implement Monitoring module** — CloudWatch, SNS, alarms
3. **Create terraform.tfvars** — Override defaults for your environment
4. **Test in isolated cluster** — Use Kind or local EKS to validate before prod
5. **Add prod environment** — Copy `environments/dev/` to `environments/prod/` and adjust variables

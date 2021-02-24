terraform {
  required_providers {
    kubernetes = {
        version                = "~> 1.9"
    }
  }
}

provider "kubernetes" {
  alias = "kube1"
  host                   = data.aws_eks_cluster.cluster1.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster1.certificate_authority.0.data)
  token                  = data.aws_eks_cluster_auth.cluster1.token
  load_config_file       = false
}

provider "kubernetes" {
  alias = "kube2"
  host                   = data.aws_eks_cluster.cluster2.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster2.certificate_authority.0.data)
  token                  = data.aws_eks_cluster_auth.cluster2.token
  load_config_file       = false
}

provider "aws" {
  region = "us-west-2"
}

provider "aws" {
  region = "us-east-1"
  alias = "east"
}

module "vpc1" {
  source          = "terraform-aws-modules/vpc/aws"
  name            = "qe-auto-vpc1-2_1_x"
  cidr            = "10.0.0.0/16"
  azs             = ["us-west-2a", "us-west-2b", "us-west-2c"]
  public_subnets  = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]

  default_security_group_egress = [
    {
      description = "Egress allowing ALL"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = "0.0.0.0/0"
    },
  ]

  default_security_group_ingress = [
    {
      description = "Ingress allowing ALL"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = "0.0.0.0/0"
    },
  ]

  enable_nat_gateway = true
  enable_vpn_gateway = true
}

resource "aws_security_group" "securitygroup1" {
  name    = "qe-auto-securitygroup1-2_1_x"
  vpc_id  = module.vpc1.vpc_id

  ingress {
    description = "Ingress allowing ALL"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    description = "Egress allowing ALL"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}

module "vpc2" {
  source          = "terraform-aws-modules/vpc/aws"
  name            = "qe-auto-vpc2-2_1_x"
  cidr            = "192.168.0.0/16"
  azs             = ["us-east-1a", "us-east-1b", "us-east-1c"]
  public_subnets  = ["192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"]

  default_security_group_egress = [
    {
      description = "Egress allowing ALL"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = "0.0.0.0/0"
    },
  ]

  default_security_group_ingress = [
    {
      description = "Ingress allowing ALL"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = "0.0.0.0/0"
    },
  ]

  enable_nat_gateway = true
  enable_vpn_gateway = true

  providers = {
    aws = aws.east
  }
}

resource "aws_security_group" "securitygroup2" {
  name      = "qe-auto-securitygroup2-2_1_x"
  provider  = aws.east
  vpc_id    = module.vpc2.vpc_id

  ingress {
    description = "Ingress allowing ALL"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    description = "Egress allowing ALL"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}

data "aws_eks_cluster" "cluster1" {
  name = module.cluster1.cluster_id
}

data "aws_eks_cluster_auth" "cluster1" {
  name = module.cluster1.cluster_id
}

module "cluster1" {
  source          = "terraform-aws-modules/eks/aws"
  cluster_name    = "qe-auto-cluster1-2_1_x"
  cluster_version = var.kubernetes-version
  subnets         = module.vpc1.public_subnets
  vpc_id          = module.vpc1.vpc_id

  cluster_security_group_id             = aws_security_group.securitygroup1.id
  worker_additional_security_group_ids  = [aws_security_group.securitygroup1.id]

  worker_groups = [
    {
      instance_type         = "m5.xlarge"
      asg_min_size          = 2
      asg_desired_capacity  = 3
      asg_max_size          = 4
      root_volume_type      = "gp2"
    }
  ]
  providers = {
    kubernetes = kubernetes.kube1
  }
}

data "aws_eks_cluster" "cluster2" {
  name      = module.cluster2.cluster_id
  provider  = aws.east
}

data "aws_eks_cluster_auth" "cluster2" {
  name      = module.cluster2.cluster_id
  provider  = aws.east
}

module "cluster2" {
  source          = "terraform-aws-modules/eks/aws"
  cluster_name    = "qe-auto-cluster2-2_1_x"
  cluster_version = var.kubernetes-version
  subnets         = module.vpc2.public_subnets
  vpc_id          = module.vpc2.vpc_id

  cluster_security_group_id             = aws_security_group.securitygroup2.id
  worker_additional_security_group_ids  = [aws_security_group.securitygroup2.id]

  worker_groups = [
    {
      instance_type         = "m5.xlarge"
      asg_min_size          = 2
      asg_desired_capacity  = 3
      asg_max_size          = 4
      root_volume_type      = "gp2"
    }
  ]
  providers = {
    kubernetes  = kubernetes.kube2
    aws         = aws.east
  }
}

module "vpc_cross_region_peering" {
  source = "github.com/grem11n/terraform-aws-vpc-peering"

  count = var.peering ? 1 : 0

  providers = {
    aws.this = aws
    aws.peer = aws.east
  }

  this_vpc_id             = module.vpc1.vpc_id
  peer_vpc_id             = module.vpc2.vpc_id

  auto_accept_peering     = true

}

output "kubeconfig1" {
  value     = module.cluster1.kubeconfig
  sensitive = true
}

output "kubeconfig2" {
  value     = module.cluster2.kubeconfig
  sensitive = true
}

terraform {
  required_providers {
    kubernetes = {
      version = "~> 2.11"
    }
  }
}

provider "aws" {
  region = "us-east-2"
  default_tags {
    tags = {
      Owner = "cao"
    }
  }
}

module "vpc" {
  source         = "terraform-aws-modules/vpc/aws"
  version        = "~>3.12.0"
  name           = "${var.name}-vpc"
  cidr           = "10.0.0.0/16"
  azs            = ["us-east-2a", "us-east-2b", "us-east-2c"]
  public_subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]

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

resource "aws_security_group" "securitygroup" {
  name   = "${var.name}-securitygroup"
  vpc_id = module.vpc.vpc_id

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

module "cluster" {
  source          = "terraform-aws-modules/eks/aws"
  version         = "~>18.26.0"
  cluster_name    = var.name
  cluster_version = var.kubernetes-version
  subnet_ids      = module.vpc.public_subnets
  vpc_id          = module.vpc.vpc_id

  cluster_security_group_id = aws_security_group.securitygroup.id

  eks_managed_node_groups = {
    default_node_group = {
      name                   = "cao-23x-pipeline"
      instance_types         = ["m5.xlarge"]
      min_size               = 11
      desired_size           = 12
      max_size               = 13
      vpc_security_group_ids = [aws_security_group.securitygroup.id]
    }
  }
}

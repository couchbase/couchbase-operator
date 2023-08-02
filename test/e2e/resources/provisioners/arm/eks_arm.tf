terraform {
  required_providers {
    kubernetes = {
      version = "~> 2.11"
    }
    aws = {
    source = "hashicorp/aws"
    version = "<= 4.67.0" # In place because of the following terraform failure in version 5.0.1:
                            # An argument named "enable_classiclink_dns_support" is not expected here.
                            # It should be changed once it is fixed.
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
      name                   = "cao-25x-pipeline"
      ami_type               = "AL2_ARM_64"
      id                     = data.aws_ami.arm-ami.id
      instance_types         = ["m6g.xlarge"]
      min_size               = 11
      desired_size           = 12
      max_size               = 13
      vpc_security_group_ids = [aws_security_group.securitygroup.id]
    }
  }
}

data "aws_ami" "arm-ami" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "architecture"
    values = ["arm64"]
  }
  filter {
    name   = "name"
    values = ["amzn2*arm64-gp2"]
  }
}

# Required for Kubernetes 1.23 and above:
# https://aws.amazon.com/blogs/containers/amazon-ebs-csi-driver-is-now-generally-available-in-amazon-eks-add-ons/
module "kubernetes_addons" {
  source = "github.com/aws-ia/terraform-aws-eks-blueprints//modules/kubernetes-addons?ref=v4.32.1"
  eks_cluster_id = module.cluster.cluster_id
  enable_amazon_eks_aws_ebs_csi_driver = true
  amazon_eks_aws_ebs_csi_driver_config = {
    addon_name               = "aws-ebs-csi-driver"
    addon_version            = "v1.17.0-eksbuild.1"
  }
 }




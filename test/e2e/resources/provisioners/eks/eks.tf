terraform {
  required_providers {
    kubernetes = {
      version = "~> 1.9"
    }
  }
}

provider "kubernetes" {
  alias                  = "kube1"
  host                   = data.aws_eks_cluster.cluster1.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster1.certificate_authority.0.data)
  token                  = data.aws_eks_cluster_auth.cluster1.token
  load_config_file       = false
}

provider "kubernetes" {
  alias                  = "kube2"
  host                   = var.remote ? data.aws_eks_cluster.cluster2[0].endpoint : null
  cluster_ca_certificate = var.remote ? base64decode(data.aws_eks_cluster.cluster2[0].certificate_authority.0.data) : null
  token                  = var.remote ? data.aws_eks_cluster_auth.cluster2[0].token : null
  load_config_file       = false
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = {
      Owner = "cao"
    }
  }
}

provider "aws" {
  region = "us-east-2"
  alias  = "east"
  default_tags {
    tags = {
      Owner = "cao"
    }
  }
}

module "vpc1" {
  source         = "terraform-aws-modules/vpc/aws"
  version        = "~>3.12.0"
  name           = "${var.name}-vpc1"
  cidr           = "10.0.0.0/16"
  azs            = ["us-east-1a", "us-east-1b", "us-east-1c"]
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

resource "aws_security_group" "securitygroup1" {
  name   = "${var.name}-securitygroup1"
  vpc_id = module.vpc1.vpc_id

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
  count = var.remote ? 1 : 0

  source         = "terraform-aws-modules/vpc/aws"
  version        = "~>3.12.0"
  name           = "${var.name}-vpc2"
  cidr           = "192.168.0.0/16"
  azs            = ["us-east-2a", "us-east-2b", "us-east-2c"]
  public_subnets = ["192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"]

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
  count = var.remote ? 1 : 0

  name     = "${var.name}-securitygroup2"
  provider = aws.east
  vpc_id   = module.vpc2[0].vpc_id

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

module "cluster1" {
  source                   = "terraform-aws-modules/eks/aws"
  version                  = "~> 17.24.0"
  cluster_name             = "${var.name}-1"
  cluster_version          = var.kubernetes-version
  subnets                  = module.vpc1.public_subnets
  vpc_id                   = module.vpc1.vpc_id
  wait_for_cluster_timeout = 600

  cluster_security_group_id            = aws_security_group.securitygroup1.id
  worker_additional_security_group_ids = [aws_security_group.securitygroup1.id]

  worker_groups = [
    {
      instance_type        = "m5.xlarge"
      asg_min_size         = 2
      asg_desired_capacity = 3
      asg_max_size         = 4
      root_volume_type     = "gp2"

      tags = [{
        key                 = "Owner"
        value               = "cao"
        propagate_at_launch = true
      }]
    }
  ]
  providers = {
    kubernetes = kubernetes.kube1
  }

}

data "aws_eks_cluster" "cluster1" {
  name = module.cluster1.cluster_id
}

data "aws_eks_cluster_auth" "cluster1" {
  name = module.cluster1.cluster_id
}

module "cluster2" {
  count = var.remote ? 1 : 0

  source                   = "terraform-aws-modules/eks/aws"
  version                  = "~> 17.24.0"
  cluster_name             = "${var.name}-2"
  cluster_version          = var.kubernetes-version
  subnets                  = module.vpc2[0].public_subnets
  vpc_id                   = module.vpc2[0].vpc_id
  wait_for_cluster_timeout = 600

  cluster_security_group_id            = aws_security_group.securitygroup2[0].id
  worker_additional_security_group_ids = [aws_security_group.securitygroup2[0].id]

  worker_groups = [
    {
      instance_type        = "m5.xlarge"
      asg_min_size         = 2
      asg_desired_capacity = 3
      asg_max_size         = 4
      root_volume_type     = "gp2"

      tags = [{
        key                 = "Owner"
        value               = "cao"
        propagate_at_launch = true
      }]
    }
  ]
  providers = {
    kubernetes = kubernetes.kube2
    aws        = aws.east
  }

}

data "aws_eks_cluster" "cluster2" {
  count    = var.remote ? 1 : 0
  name     = module.cluster2[0].cluster_id
  provider = aws.east
}

data "aws_eks_cluster_auth" "cluster2" {
  count    = var.remote ? 1 : 0
  name     = module.cluster2[0].cluster_id
  provider = aws.east
}

resource "aws_vpc_peering_connection" "peering" {
  count = var.remote ? 1 : 0

  vpc_id      = module.vpc1.vpc_id
  peer_vpc_id = module.vpc2[0].vpc_id
  peer_region = "us-east-1"
  auto_accept = false
}

resource "aws_vpc_peering_connection_accepter" "peering-accept" {
  count = var.remote ? 1 : 0

  provider                  = aws.east
  vpc_peering_connection_id = aws_vpc_peering_connection.peering[0].id
  auto_accept               = true
}


resource "aws_route" "peering-route-1" {
  count = var.remote ? 1 : 0

  route_table_id            = module.vpc1.public_route_table_ids[0]
  destination_cidr_block    = "192.168.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.peering[0].id
}

resource "aws_route" "peering-route-2" {
  count = var.remote ? 1 : 0

  route_table_id            = module.vpc2[0].public_route_table_ids[0]
  destination_cidr_block    = "10.0.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.peering[0].id
  provider                  = aws.east
}

output "kubeconfig1" {
  value     = module.cluster1.kubeconfig
  sensitive = true
}

output "kubeconfig2" {
  value     = var.remote ? module.cluster2[0].kubeconfig : null
  sensitive = true
}

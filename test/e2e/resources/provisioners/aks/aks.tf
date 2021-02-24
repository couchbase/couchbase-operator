# Configure the Azure Provider
terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "=2.40.0"
    }
  }
}

provider "azurerm" {
  subscription_id = var.subscription-id
  client_id       = var.service-principal-id
  client_secret   = var.service-principal-secret
  tenant_id       = var.tenant-id
  features {}
}

# Create resource group
resource "azurerm_resource_group" "qe-auto" {
  name     = "qe-auto-21x"
  location = "East US 2"
}


# Create VNet 1
resource "azurerm_virtual_network" "qe-auto-vnet1" {
  name                = "qe-auto-vnet1-21x"
  location            = "East US 2"
  resource_group_name = azurerm_resource_group.qe-auto.name
  address_space       = ["10.0.0.0/12"]
}

# Create Subnet for VNet 1
resource "azurerm_subnet" "qe-auto-vnet1-subnet" {
  name                  = "qe-auto-vnet1-subnet-21x"
  resource_group_name   = azurerm_resource_group.qe-auto.name
  virtual_network_name  = azurerm_virtual_network.qe-auto-vnet1.name
  address_prefixes      = ["10.8.0.0/16"]
}

# Create Cluster 1
resource "azurerm_kubernetes_cluster" "qe-auto-cluster1" {
  name                = "qe-auto-cluster1-21x"
  location            = "East US 2"
  resource_group_name = azurerm_resource_group.qe-auto.name
  dns_prefix          = "qe-auto-cluster1-21x"
  kubernetes_version  = var.kubernetes-version

  default_node_pool {
    name                = "nodepool21x1"
    node_count          = 3
    vm_size             = "Standard_D4s_v4"
    availability_zones  = ["1", "2", "3"]
    os_disk_size_gb     = 30
    vnet_subnet_id      = azurerm_subnet.qe-auto-vnet1-subnet.id
  }

  network_profile {
    network_plugin      = "azure"
    dns_service_ip      = "10.0.0.10"
    docker_bridge_cidr  = "172.17.0.1/16"
    service_cidr        = "10.0.0.0/16"
  }

  addon_profile {
    http_application_routing {
      enabled = true
    }
  }

  service_principal {
    client_id     = var.service-principal-id
    client_secret = var.service-principal-secret
  }

}

# Do it all again for a 2nd cluster!

# Create VNet 2
resource "azurerm_virtual_network" "qe-auto-vnet2" {
  name                = "qe-auto-vnet2-21x"
  location            = "West US 2"
  resource_group_name = azurerm_resource_group.qe-auto.name
  address_space       = ["10.16.0.0/12"]
}

# Create Subnet for VNet 2
resource "azurerm_subnet" "qe-auto-vnet2-subnet" {
  name                  = "qe-auto-vnet2-subnet-21x"
  resource_group_name   = azurerm_resource_group.qe-auto.name
  virtual_network_name  = azurerm_virtual_network.qe-auto-vnet2.name
  address_prefixes      = ["10.24.0.0/16"]
}


# Create Cluster 2
resource "azurerm_kubernetes_cluster" "qe-auto-cluster2" {
  name                = "qe-auto-cluster2-21x"
  location            = "West US 2"
  resource_group_name = azurerm_resource_group.qe-auto.name
  dns_prefix          = "qe-auto-cluster2-21x"
  kubernetes_version  = var.kubernetes-version

  default_node_pool {
    name                = "nodepool21x2"
    node_count          = 3
    vm_size             = "Standard_D4s_v4"
    availability_zones  = ["1", "2", "3"]
    os_disk_size_gb     = 30
    vnet_subnet_id      = azurerm_subnet.qe-auto-vnet2-subnet.id
  }

  network_profile {
    network_plugin      = "azure"
    dns_service_ip      = "10.16.0.10"
    docker_bridge_cidr  = "172.17.0.1/16"
    service_cidr        = "10.16.0.0/16"
  }

  addon_profile {
    http_application_routing {
      enabled = true
    }
  }

  service_principal {
    client_id     = var.service-principal-id
    client_secret = var.service-principal-secret
  }

}

# Peer the VNets so they can chat

resource "azurerm_virtual_network_peering" "qe-auto-peer-1to2" {
  name = "qe-auto-peer-1to2"
  resource_group_name       = azurerm_resource_group.qe-auto.name
  virtual_network_name      = azurerm_virtual_network.qe-auto-vnet1.name
  remote_virtual_network_id = azurerm_virtual_network.qe-auto-vnet2.id
  allow_forwarded_traffic   = true
}

resource "azurerm_virtual_network_peering" "qe-auto-peer-2to1" {
  name = "qe-auto-peer-2to1"
  resource_group_name       = azurerm_resource_group.qe-auto.name
  virtual_network_name      = azurerm_virtual_network.qe-auto-vnet2.name
  remote_virtual_network_id = azurerm_virtual_network.qe-auto-vnet1.id
  allow_forwarded_traffic   = true
}

# Output the kubeconfigs for the two clusters

output "kubeconfig1" {
  value = azurerm_kubernetes_cluster.qe-auto-cluster1.kube_config_raw
  sensitive = true
}

output "kubeconfig2" {
  value = azurerm_kubernetes_cluster.qe-auto-cluster2.kube_config_raw
  sensitive = true
}

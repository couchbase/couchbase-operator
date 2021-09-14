# Configure the Azure Provider
terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "=2.40.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.0"
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
resource "azurerm_resource_group" "resourcegroup" {
  name     = var.name
  location = "East US 2"
}


# Create VNet 1
resource "azurerm_virtual_network" "vnet1" {
  name                = "${var.name}-vnet1"
  location            = "East US 2"
  resource_group_name = azurerm_resource_group.resourcegroup.name
  address_space       = ["10.0.0.0/12"]
}

# Create Subnet for VNet 1
resource "azurerm_subnet" "vnet1-subnet" {
  name                  = "${var.name}-vnet1-subnet"
  resource_group_name   = azurerm_resource_group.resourcegroup.name
  virtual_network_name  = azurerm_virtual_network.vnet1.name
  address_prefixes      = ["10.8.0.0/16"]
}

# Create Cluster 1
resource "azurerm_kubernetes_cluster" "cluster1" {
  name                = "${var.name}-cluster1"
  location            = "East US 2"
  resource_group_name = azurerm_resource_group.resourcegroup.name
  dns_prefix          = "${var.name}-cluster1"
  kubernetes_version  = var.kubernetes-version

  default_node_pool {
    name                = "nodepool1"
    node_count          = 6
    vm_size             = "Standard_D4s_v4"
    availability_zones  = ["1", "2", "3"]
    os_disk_size_gb     = 30
    vnet_subnet_id      = azurerm_subnet.vnet1-subnet.id
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
resource "azurerm_virtual_network" "vnet2" {
  count = var.remote ? 1 : 0

  name                = "${var.name}-vnet2"
  location            = "West US 2"
  resource_group_name = azurerm_resource_group.resourcegroup.name
  address_space       = ["10.16.0.0/12"]
}

# Create Subnet for VNet 2
resource "azurerm_subnet" "vnet2-subnet" {
  count = var.remote ? 1 : 0

  name                  = "${var.name}vnet2-subnet"
  resource_group_name   = azurerm_resource_group.resourcegroup.name
  virtual_network_name  = azurerm_virtual_network.vnet2[0].name
  address_prefixes      = ["10.24.0.0/16"]
}


# Create Cluster 2
resource "azurerm_kubernetes_cluster" "cluster2" {
  count = var.remote ? 1 : 0

  name                = "${var.name}-cluster2"
  location            = "West US 2"
  resource_group_name = azurerm_resource_group.resourcegroup.name
  dns_prefix          = "${var.name}-cluster2"
  kubernetes_version  = var.kubernetes-version

  default_node_pool {
    name                = "nodepool2"
    node_count          = 6
    vm_size             = "Standard_D4s_v4"
    availability_zones  = ["1", "2", "3"]
    os_disk_size_gb     = 30
    vnet_subnet_id      = azurerm_subnet.vnet2-subnet[0].id
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

resource "azurerm_virtual_network_peering" "peer-1to2" {
  count = var.remote ? 1 : 0

  name = "${var.name}-peer-1to2"
  resource_group_name       = azurerm_resource_group.resourcegroup.name
  virtual_network_name      = azurerm_virtual_network.vnet1.name
  remote_virtual_network_id = azurerm_virtual_network.vnet2[0].id
  allow_forwarded_traffic   = true
}

resource "azurerm_virtual_network_peering" "peer-2to1" {
  count = var.remote ? 1 : 0

  name = "${var.name}-peer-2to1"
  resource_group_name       = azurerm_resource_group.resourcegroup.name
  virtual_network_name      = azurerm_virtual_network.vnet2[0].name
  remote_virtual_network_id = azurerm_virtual_network.vnet1.id
  allow_forwarded_traffic   = true
}

# Output the kubeconfigs for the two clusters

output "kubeconfig1" {
  value = azurerm_kubernetes_cluster.cluster1.kube_config_raw
  sensitive = true
}

output "kubeconfig2" {
  value = var.remote ? azurerm_kubernetes_cluster.cluster2[0].kube_config_raw : null
  sensitive = true
}

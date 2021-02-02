variable "service-principal-id" {
  type      = string
  sensitive = true
}

variable "service-principal-secret" {
  type      = string
  sensitive = true
}

variable "subscription-id" {
  type      = string
  sensitive = true
}

variable "tenant-id" {
  type      = string
  sensitive = true
}

variable "kubernetes-version" {
  type    = string
  default = "1.17.13"
}

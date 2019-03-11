{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "test-resources.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "test-resources.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "test-resources.ci-fullname" -}}
{{- if .Values.testID -}}
  {{- printf "%s-%s" (include "test-resources.fullname" .) .Values.testID -}}
{{- else -}}
  {{- (include "test-resources.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "test-resources.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Get kind of rbac role to use based on requested level of access
*/}}
{{- define "test-resources.rbacRole" -}}
{{- if .Values.rbac.clusterRoleAccess -}}
{{- printf "ClusterRole" }}
{{- else -}}
{{- printf "Role" }}
{{- end -}}
{{- end -}}

{{/*
Name of the service account used for operator and admission controller.
*/}}
{{- define "test-resources.serviceaccount" -}}
{{- default (include "test-resources.fullname" .) .Values.rbac.serviceAccountName | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Name of the service account used by test Pods.
*/}}
{{- define "test-resources.ci-serviceaccount" -}}
{{- printf "ci-%s" (include "test-resources.serviceaccount" .) -}}
{{- end -}}

{{/*
Name of the tls secret.
*/}}
{{- define "test-resources.secretname" -}}
{{- default (include "test-resources.fullname" .) .Values.tls.secretName | trunc 63 | trimSuffix "-" -}}
{{- end -}}


{{/*
Name of the cluster
*/}}
{{- define "test-resources.adminconsole" -}}
{{- $clustername := default .Chart.Name ((index .Values "couchbase-cluster") | .couchbaseCluster.name) -}}
{{- ( printf "%s.%s.svc" $clustername .Release.Namespace ) -}}
{{- end -}}

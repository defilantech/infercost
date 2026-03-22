{{/*
Expand the name of the chart.
*/}}
{{- define "infercost.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "infercost.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "infercost.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "infercost.labels" -}}
helm.sh/chart: {{ include "infercost.chart" . }}
{{ include "infercost.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "infercost.selectorLabels" -}}
app.kubernetes.io/name: {{ include "infercost.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "infercost.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-controller-manager" (include "infercost.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the controller manager image
*/}}
{{- define "infercost.controllerImage" -}}
{{- if .Values.controllerManager.image.digest }}
{{- printf "%s@%s" .Values.controllerManager.image.repository .Values.controllerManager.image.digest }}
{{- else }}
{{- $tag := .Values.controllerManager.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.controllerManager.image.repository $tag }}
{{- end }}
{{- end }}

{{/*
Create the namespace
*/}}
{{- define "infercost.namespace" -}}
{{- default .Values.namespace .Release.Namespace }}
{{- end }}

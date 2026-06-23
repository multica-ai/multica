{{/*
Expand the name of the chart.
*/}}
{{- define "multica.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Defaults to the release name so the deployed name matches the Helm release.
*/}}
{{- define "multica.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Namespace all resources are deployed into.
Defaults to .Values.namespace (costrict-web); falls back to the release
namespace (the -n flag) when that value is explicitly cleared.
*/}}
{{- define "multica.namespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "multica.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "multica.labels" -}}
helm.sh/chart: {{ include "multica.chart" . }}
{{ include "multica.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "multica.selectorLabels" -}}
app.kubernetes.io/name: {{ include "multica.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Backend selector labels
*/}}
{{- define "multica.backendSelectorLabels" -}}
app.kubernetes.io/name: {{ include "multica.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Web selector labels
*/}}
{{- define "multica.webSelectorLabels" -}}
app.kubernetes.io/name: {{ include "multica.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: web
{{- end }}

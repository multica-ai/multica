{{/*
Expand the name of the chart.
*/}}
{{- define "multica.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "multica.fullname" -}}
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
Chart name and version.
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
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
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
Backend component labels
*/}}
{{- define "multica.backend.labels" -}}
{{ include "multica.labels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{- define "multica.backend.selectorLabels" -}}
{{ include "multica.selectorLabels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Frontend component labels
*/}}
{{- define "multica.frontend.labels" -}}
{{ include "multica.labels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "multica.frontend.selectorLabels" -}}
{{ include "multica.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Postgres component labels
*/}}
{{- define "multica.postgres.labels" -}}
{{ include "multica.labels" . }}
app.kubernetes.io/component: postgresql
{{- end }}

{{- define "multica.postgres.selectorLabels" -}}
{{ include "multica.selectorLabels" . }}
app.kubernetes.io/component: postgresql
{{- end }}

{{/*
Resource names
*/}}
{{- define "multica.backend.fullname" -}}
{{- printf "%s-backend" (include "multica.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "multica.frontend.fullname" -}}
{{- printf "%s-frontend" (include "multica.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "multica.postgres.fullname" -}}
{{- printf "%s-postgres" (include "multica.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Service account name
*/}}
{{- define "multica.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "multica.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Secret name (holds credentials)
*/}}
{{- define "multica.secretName" -}}
{{- printf "%s-secrets" (include "multica.fullname" .) }}
{{- end }}

{{/*
ConfigMap name
*/}}
{{- define "multica.configMapName" -}}
{{- printf "%s-config" (include "multica.fullname" .) }}
{{- end }}

{{/*
Backend image (tag falls back to global)
*/}}
{{- define "multica.backend.image" -}}
{{- $tag := .Values.backend.image.tag | default .Values.global.imageTag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.backend.image.repository $tag }}
{{- end }}

{{/*
Frontend image
*/}}
{{- define "multica.frontend.image" -}}
{{- $tag := .Values.frontend.image.tag | default .Values.global.imageTag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.frontend.image.repository $tag }}
{{- end }}

{{/*
Postgres image
*/}}
{{- define "multica.postgres.image" -}}
{{- printf "%s:%s" .Values.postgresql.image.repository .Values.postgresql.image.tag }}
{{- end }}

{{/*
Postgres host (service name)
*/}}
{{- define "multica.postgres.host" -}}
{{- include "multica.postgres.fullname" . }}
{{- end }}

{{/*
DATABASE_URL built from postgres config.
When postgresql.enabled=false, config.externalDatabaseUrl must be set.
*/}}
{{- define "multica.databaseUrl" -}}
{{- if .Values.postgresql.enabled -}}
postgres://{{ .Values.postgresql.auth.username }}:$(POSTGRES_PASSWORD)@{{ include "multica.postgres.host" . }}:{{ .Values.postgresql.service.port }}/{{ .Values.postgresql.auth.database }}?sslmode=disable
{{- else -}}
{{- required "config.externalDatabaseUrl is required when postgresql.enabled=false" .Values.config.externalDatabaseUrl -}}
{{- end -}}
{{- end }}

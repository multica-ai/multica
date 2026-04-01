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
Create chart label.
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
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "multica.selectorLabels" -}}
app.kubernetes.io/name: {{ include "multica.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Server-specific labels
*/}}
{{- define "multica.server.labels" -}}
{{ include "multica.labels" . }}
app.kubernetes.io/component: server
{{- end }}

{{- define "multica.server.selectorLabels" -}}
{{ include "multica.selectorLabels" . }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Web-specific labels
*/}}
{{- define "multica.web.labels" -}}
{{ include "multica.labels" . }}
app.kubernetes.io/component: web
{{- end }}

{{- define "multica.web.selectorLabels" -}}
{{ include "multica.selectorLabels" . }}
app.kubernetes.io/component: web
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
Server image
*/}}
{{- define "multica.server.image" -}}
{{- $tag := .Values.server.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.server.image.repository $tag }}
{{- end }}

{{/*
Web image
*/}}
{{- define "multica.web.image" -}}
{{- $tag := .Values.web.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.web.image.repository $tag }}
{{- end }}

{{/*
Database URL env source — from secret or postgresql subchart
*/}}
{{- define "multica.databaseUrlEnv" -}}
{{- if .Values.externalDatabase.existingSecret }}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.externalDatabase.existingSecret }}
      key: {{ .Values.externalDatabase.existingSecretKey }}
{{- else if .Values.externalDatabase.url }}
- name: DATABASE_URL
  value: {{ .Values.externalDatabase.url | quote }}
{{- else if .Values.postgresql.enabled }}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      {{- if .Values.postgresql.auth.existingSecret }}
      name: {{ .Values.postgresql.auth.existingSecret }}
      key: password
      {{- else }}
      name: {{ include "multica.fullname" . }}-postgresql
      key: password
      {{- end }}
- name: DATABASE_URL
  value: "postgres://{{ .Values.postgresql.auth.username }}:$(POSTGRES_PASSWORD)@{{ include "multica.fullname" . }}-postgresql:5432/{{ .Values.postgresql.auth.database }}?sslmode=disable"
{{- end }}
{{- end }}

{{/*
JWT secret env source
*/}}
{{- define "multica.jwtSecretEnv" -}}
{{- if .Values.jwt.existingSecret }}
- name: JWT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.jwt.existingSecret }}
      key: {{ .Values.jwt.existingSecretKey }}
{{- else if .Values.jwt.secret }}
- name: JWT_SECRET
  value: {{ .Values.jwt.secret | quote }}
{{- end }}
{{- end }}

{{/*
Server service name
*/}}
{{- define "multica.server.serviceName" -}}
{{- printf "%s-server" (include "multica.fullname" .) }}
{{- end }}

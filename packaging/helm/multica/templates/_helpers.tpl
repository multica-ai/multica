{{/* vim: set filetype=mustache: */}}

{{/*
Common labels applied to every resource.
*/}}
{{- define "multica.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: multica
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{/*
Component-scoped labels — usage:
  {{- include "multica.componentLabels" (dict "name" "backend" "ctx" .) | nindent 4 }}
*/}}
{{- define "multica.componentLabels" -}}
{{ include "multica.labels" .ctx }}
app.kubernetes.io/component: {{ .name }}
app.kubernetes.io/name: {{ printf "multica-%s" .name }}
{{- end }}

{{/*
Selector labels — minimal stable subset of componentLabels for matchLabels/selector.
  {{- include "multica.componentSelector" (dict "name" "backend" "ctx" .) | nindent 6 }}
*/}}
{{- define "multica.componentSelector" -}}
app.kubernetes.io/name: {{ printf "multica-%s" .name }}
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
app.kubernetes.io/component: {{ .name }}
{{- end }}

{{/*
Resolve an image reference: image.tags.<key> overrides image.tag.
  {{ include "multica.image" (dict "key" "backend" "ctx" .) }}
*/}}
{{- define "multica.image" -}}
{{- $img := .ctx.Values.image -}}
{{- $key := .key -}}
{{- $tag := default $img.tag (index $img.tags $key) -}}
{{- printf "%s/multica-%s:%s" $img.registry $key $tag -}}
{{- end }}

{{/*
Reference to the platform Postgres URL — used by the backend Deployment.
*/}}
{{- define "multica.databaseUrl" -}}
{{- if .Values.platform.postgres.enabled -}}
postgres://{{ .Values.platform.postgres.user }}:$(POSTGRES_PASSWORD)@multica-postgres:5432/{{ .Values.platform.postgres.database }}?sslmode=disable
{{- else -}}
{{ required "platform.postgres.externalUrl is required when platform.postgres.enabled=false" .Values.platform.postgres.externalUrl }}
{{- end -}}
{{- end }}

{{/*
Resolve the runtime (agent) image reference.
  {{ include "multica.runtimeImage" . }}
*/}}
{{- define "multica.runtimeImage" -}}
{{- $img := .Values.runtime.daemon.image -}}
{{- $tag := default .Values.image.tag $img.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry $img.name $tag -}}
{{- end }}

{{/*
Resolve the controller image reference.
  {{ include "multica.controllerImage" . }}
*/}}
{{- define "multica.controllerImage" -}}
{{- $img := .Values.runtime.controller.image -}}
{{- $tag := default .Values.image.tag $img.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry $img.name $tag -}}
{{- end }}

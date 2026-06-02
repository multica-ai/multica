{{/*
Common labels for all resources.
*/}}
{{- define "wallts.labels" -}}
app.kubernetes.io/name: wallts
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Per-component resource names. Using Release.Name keeps the same name we used
under the kustomize layout when installed as `helm install wallts ...`.
*/}}
{{- define "wallts.backend.fullname" -}}
{{ .Release.Name }}-backend
{{- end -}}

{{- define "wallts.frontend.fullname" -}}
{{ .Release.Name }}-frontend
{{- end -}}

{{- define "wallts.postgres.fullname" -}}
{{ .Release.Name }}-postgres
{{- end -}}

{{/*
DATABASE_URL for the backend container.

When postgresql.external.databaseUrl is set, it takes priority (external DB).
Otherwise the URL is assembled from the bundled postgres Service + Secret env
vars. The $(VAR) syntax is resolved by the kubelet from the container's env,
so POSTGRES_USER / POSTGRES_PASSWORD / POSTGRES_DB must also be loaded into
env on the same container (see envFrom on the backend Deployment).
*/}}
{{- define "wallts.databaseUrl" -}}
{{- if .Values.postgresql.external.databaseUrl -}}
{{ .Values.postgresql.external.databaseUrl }}
{{- else -}}
postgres://$(POSTGRES_USER):***@{{ include "wallts.postgres.fullname" . }}:5432/$(POSTGRES_DB)?sslmode=disable
{{- end -}}
{{- end -}}

{{/*
Common labels for all resources.
*/}}
{{- define "multica.labels" -}}
app.kubernetes.io/name: multica
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Per-component resource names. Using Release.Name keeps the same name we used
under the kustomize layout when installed as `helm install multica ...`.
*/}}
{{- define "multica.backend.fullname" -}}
{{ .Release.Name }}-backend
{{- end -}}

{{- define "multica.frontend.fullname" -}}
{{ .Release.Name }}-frontend
{{- end -}}

{{- define "multica.postgres.fullname" -}}
{{ .Release.Name }}-postgres
{{- end -}}

{{/*
DATABASE_URL pieced together from the postgres service + Secret values.
The $(VAR) syntax is resolved by the kubelet from the container's env, so
POSTGRES_USER / POSTGRES_PASSWORD / POSTGRES_DB must also be loaded into env
on the same container (see envFrom on the backend Deployment).
*/}}
{{- define "multica.databaseUrl" -}}
postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@{{ include "multica.postgres.fullname" . }}:5432/$(POSTGRES_DB)?sslmode=disable
{{- end -}}

{{/*
Render additional container env entries.
Each item supports Kubernetes EnvVar fields. String `value` fields are passed
through tpl so operators can reference chart values in custom environment.
*/}}
{{- define "multica.extraEnv" -}}
{{- $root := .root -}}
{{- range .env }}
- name: {{ required "extraEnv entries require a name" .name | quote }}
  {{- if hasKey . "valueFrom" }}
  valueFrom:
    {{- tpl (toYaml .valueFrom) $root | nindent 4 }}
  {{- else if hasKey . "value" }}
  value: {{ tpl (toString .value) $root | quote }}
  {{- else }}
  value: ""
  {{- end }}
{{- end }}
{{- end -}}

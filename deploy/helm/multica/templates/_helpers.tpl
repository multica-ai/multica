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

{{/* ServiceAccount shared by all chart workloads. */}}
{{- define "multica.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default .Release.Name .Values.serviceAccount.name -}}
{{- else -}}
{{- /* create=false: use the named SA, or fall back to the namespace "default" SA (matches pre-ServiceAccount chart behavior). */ -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Immutable digest references take precedence over tags. */}}
{{- define "multica.backend.image" -}}
{{- if .Values.images.backend.digest -}}
{{ .Values.images.backend.repository }}@{{ .Values.images.backend.digest }}
{{- else -}}
{{ .Values.images.backend.repository }}:{{ default .Chart.AppVersion .Values.images.backend.tag }}
{{- end -}}
{{- end -}}

{{- define "multica.frontend.image" -}}
{{- if .Values.images.frontend.digest -}}
{{ .Values.images.frontend.repository }}@{{ .Values.images.frontend.digest }}
{{- else -}}
{{ .Values.images.frontend.repository }}:{{ default .Chart.AppVersion .Values.images.frontend.tag }}
{{- end -}}
{{- end -}}

{{- define "multica.postgres.image" -}}
{{- if .Values.images.postgres.digest -}}
{{ .Values.images.postgres.repository }}@{{ .Values.images.postgres.digest }}
{{- else -}}
{{ .Values.images.postgres.repository }}:{{ default .Chart.AppVersion .Values.images.postgres.tag }}
{{- end -}}
{{- end -}}

{{/* System pod labels cannot be replaced by user-provided labels. */}}
{{- define "multica.podLabels" -}}
{{- $system := fromYaml (include "multica.labels" .root) -}}
{{- $_ := set $system "app.kubernetes.io/component" .component -}}
{{- $labels := mergeOverwrite (deepCopy (default dict .custom)) $system -}}
{{- toYaml $labels -}}
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

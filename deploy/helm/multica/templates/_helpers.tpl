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

{{- define "multica.collab.fullname" -}}
{{ .Release.Name }}-collab
{{- end -}}

{{/*
"true" when the collab service is deployed, otherwise empty. `helm upgrade
--reuse-values` from a chart without the collab section carries no such key.
*/}}
{{- define "multica.collab.enabled" -}}
{{- if (default dict .Values.collab).enabled -}}true{{- end -}}
{{- end -}}

{{/*
The CDN domain the backend reports (Storage.CdnDomain): CLOUDFRONT_DOMAIN with
S3, otherwise the host of LOCAL_UPLOAD_BASE_URL.
*/}}
{{- define "multica.collab.cdnDomain" -}}
{{- $config := default dict (default dict .Values.collab).config -}}
{{- if $config.cdnDomain -}}
{{ $config.cdnDomain }}
{{- else if .Values.backend.config.s3Bucket -}}
{{ .Values.backend.config.cloudfrontDomain }}
{{- else if .Values.backend.config.localUploadBaseUrl -}}
{{ (urlParse .Values.backend.config.localUploadBaseUrl).hostname }}
{{- end -}}
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

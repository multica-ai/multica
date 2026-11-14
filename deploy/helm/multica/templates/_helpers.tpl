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
Container image reference. Renders repository@digest when a digest is set and
repository:tag otherwise, so existing tag-only values render unchanged. The
digest must be a full sha256 digest (for a multi-arch release, use the OCI
index digest). Anything else fails `helm template` / `helm install` before
rollout instead of producing an image reference the kubelet cannot pull.

Usage: include "multica.image" (dict "name" "backend" "image" .Values.images.backend "tag" $tag)
*/}}
{{- define "multica.image" -}}
{{- $digest := .image.digest | default "" | trim -}}
{{- if $digest -}}
{{- if not (regexMatch "^sha256:[a-f0-9]{64}$" $digest) -}}
{{- fail (printf "images.%s.digest must be a sha256 digest such as sha256:<64 lowercase hex characters>, got %q" .name $digest) -}}
{{- end -}}
{{- printf "%s@%s" .image.repository $digest -}}
{{- else -}}
{{- printf "%s:%s" .image.repository .tag -}}
{{- end -}}
{{- end -}}

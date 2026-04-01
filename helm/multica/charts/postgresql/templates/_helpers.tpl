{{/*
Copyright Broadcom, Inc. All Rights Reserved.
SPDX-License-Identifier: APACHE-2.0
*/}}

{{/* vim: set filetype=mustache: */}}

{{/*
Create a global name for the chart to use and parse with other naming functions
Please use instead of "common.names.fullname" to preserve support for .Values.global.postgresql.fullnameOverride
*/}}
{{- define "postgresql.v1.chart.fullname" -}}
{{- default (include "common.names.fullname" .) .Values.global.postgresql.fullnameOverride -}}
{{- end -}}

{{/*
Create a default fully qualified app name for PostgreSQL Primary objects
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "postgresql.v1.primary.fullname" -}}
{{- $fullname := include "postgresql.v1.chart.fullname" . -}}
{{- ternary (printf "%s-%s" $fullname .Values.primary.name | trunc 63 | trimSuffix "-") $fullname (eq .Values.architecture "replication") -}}
{{- end -}}

{{/*
Create a default fully qualified app name for PostgreSQL read-only replicas objects
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "postgresql.v1.readReplica.fullname" -}}
{{- printf "%s-%s" (include "postgresql.v1.chart.fullname" .) .Values.readReplicas.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the default FQDN for PostgreSQL primary headless service
We truncate at 63 chars because of the DNS naming spec.
*/}}
{{- define "postgresql.v1.primary.svc.headless" -}}
{{- printf "%s-hl" (include "postgresql.v1.primary.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the default FQDN for PostgreSQL read-only replicas headless service
We truncate at 63 chars because of the DNS naming spec.
*/}}
{{- define "postgresql.v1.readReplica.svc.headless" -}}
{{- printf "%s-hl" (include "postgresql.v1.readReplica.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Return the proper PostgreSQL image name
*/}}
{{- define "postgresql.v1.image" -}}
{{ include "common.images.image" (dict "imageRoot" .Values.image "global" .Values.global) }}
{{- end -}}

{{/*
Return the proper PostgreSQL metrics image name
*/}}
{{- define "postgresql.v1.metrics.image" -}}
{{ include "common.images.image" (dict "imageRoot" .Values.metrics.image "global" .Values.global) }}
{{- end -}}

{{/*
Return the proper image name (for the init container volume-permissions image)
*/}}
{{- define "postgresql.v1.volumePermissions.image" -}}
{{ include "common.images.image" (dict "imageRoot" .Values.volumePermissions.image "global" .Values.global) }}
{{- end -}}

{{/*
Return the proper Docker Image Registry Secret Names
*/}}
{{- define "postgresql.v1.imagePullSecrets" -}}
{{ include "common.images.renderPullSecrets" (dict "images" (list .Values.image .Values.metrics.image .Values.volumePermissions.image) "context" $) }}
{{- end -}}

{{/*
Return the name for a custom user to create
*/}}
{{- define "postgresql.v1.username" -}}
{{- if .Values.global.postgresql.auth.username -}}
    {{- .Values.global.postgresql.auth.username -}}
{{- else -}}
    {{- .Values.auth.username -}}
{{- end -}}
{{- end -}}

{{/*
Return the name for a custom database to create
*/}}
{{- define "postgresql.v1.database" -}}
{{- if .Values.global.postgresql.auth.database -}}
    {{- printf "%s" (tpl .Values.global.postgresql.auth.database $) -}}
{{- else if .Values.auth.database -}}
    {{- printf "%s" (tpl .Values.auth.database $) -}}
{{- end -}}
{{- end -}}

{{/*
Get the password secret.
*/}}
{{- define "postgresql.v1.secretName" -}}
{{- if .Values.global.postgresql.auth.existingSecret -}}
    {{- printf "%s" (tpl .Values.global.postgresql.auth.existingSecret $) -}}
{{- else if .Values.auth.existingSecret -}}
    {{- printf "%s" (tpl .Values.auth.existingSecret $) -}}
{{- else -}}
    {{- printf "%s" (include "postgresql.v1.chart.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Get the replication-password key.
*/}}
{{- define "postgresql.v1.replicationPasswordKey" -}}
{{- if or .Values.global.postgresql.auth.existingSecret .Values.auth.existingSecret -}}
    {{- if .Values.global.postgresql.auth.secretKeys.replicationPasswordKey -}}
        {{- printf "%s" (tpl .Values.global.postgresql.auth.secretKeys.replicationPasswordKey $) -}}
    {{- else if .Values.auth.secretKeys.replicationPasswordKey -}}
        {{- printf "%s" (tpl .Values.auth.secretKeys.replicationPasswordKey $) -}}
    {{- else -}}
        {{- "replication-password" -}}
    {{- end -}}
{{- else -}}
    {{- "replication-password" -}}
{{- end -}}
{{- end -}}

{{/*
Get the admin-password key.
*/}}
{{- define "postgresql.v1.adminPasswordKey" -}}
{{- if or .Values.global.postgresql.auth.existingSecret .Values.auth.existingSecret -}}
    {{- if .Values.global.postgresql.auth.secretKeys.adminPasswordKey -}}
        {{- printf "%s" (tpl .Values.global.postgresql.auth.secretKeys.adminPasswordKey $) -}}
    {{- else if .Values.auth.secretKeys.adminPasswordKey -}}
        {{- printf "%s" (tpl .Values.auth.secretKeys.adminPasswordKey $) -}}
    {{- end -}}
{{- else -}}
    {{- "postgres-password" -}}
{{- end -}}
{{- end -}}

{{/*
Get the user-password key.
*/}}
{{- define "postgresql.v1.userPasswordKey" -}}
{{- if or .Values.global.postgresql.auth.existingSecret .Values.auth.existingSecret -}}
    {{- if or (empty (include "postgresql.v1.username" .)) (eq (include "postgresql.v1.username" .) "postgres") -}}
        {{- printf "%s" (include "postgresql.v1.adminPasswordKey" .) -}}
    {{- else -}}
        {{- if .Values.global.postgresql.auth.secretKeys.userPasswordKey -}}
            {{- printf "%s" (tpl .Values.global.postgresql.auth.secretKeys.userPasswordKey $) -}}
        {{- else if .Values.auth.secretKeys.userPasswordKey -}}
            {{- printf "%s" (tpl .Values.auth.secretKeys.userPasswordKey $) -}}
        {{- end -}}
    {{- end -}}
{{- else -}}
    {{- "password" -}}
{{- end -}}
{{- end -}}

{{/*
Return true if a secret object should be created
*/}}
{{- define "postgresql.v1.createSecret" -}}
{{- $customUser := include "postgresql.v1.username" . -}}
{{- $postgresPassword := include "common.secrets.lookup" (dict "secret" (include "postgresql.v1.chart.fullname" .) "key" .Values.auth.secretKeys.adminPasswordKey "defaultValue" (ternary (coalesce .Values.global.postgresql.auth.postgresPassword .Values.auth.postgresPassword .Values.global.postgresql.auth.password .Values.auth.password) (coalesce .Values.global.postgresql.auth.postgresPassword .Values.auth.postgresPassword) (or (empty $customUser) (eq $customUser "postgres"))) "context" $) -}}
{{- if and (not (or .Values.global.postgresql.auth.existingSecret .Values.auth.existingSecret)) (or $postgresPassword .Values.auth.enablePostgresUser (and (not (empty $customUser)) (ne $customUser "postgres")) (eq .Values.architecture "replication") (and .Values.ldap.enabled (or .Values.ldap.bind_password .Values.ldap.bindpw))) -}}
    {{- true -}}
{{- end -}}
{{- end -}}

{{/*
Return true if a secret object should be created for PostgreSQL
*/}}
{{- define "postgresql.v1.createPreviousSecret" -}}
{{- if and .Values.passwordUpdateJob.previousPasswords.postgresPassword (not .Values.passwordUpdateJob.previousPasswords.existingSecret) }}
    {{- true -}}
{{- end -}}
{{- end -}}

{{/*
Return the secret with previous PostgreSQL credentials
*/}}
{{- define "postgresql.v1.update-job.previousSecretName" -}}
    {{- if .Values.passwordUpdateJob.previousPasswords.existingSecret -}}
        {{- /* The secret with the new password is managed externally */ -}}
        {{- tpl .Values.passwordUpdateJob.previousPasswords.existingSecret $ -}}
    {{- else if .Values.passwordUpdateJob.previousPasswords.postgresPassword -}}
        {{- /* The secret with the new password is managed externally */ -}}
        {{- printf "%s-previous-secret" (include "postgresql.v1.chart.fullname" .) | trunc 63 | trimSuffix "-" -}}
    {{- else -}}
        {{- /* The secret with the new password is managed by the helm chart. We use the current secret name as it has the old password */ -}}
        {{- include "postgresql.v1.chart.fullname" . -}}
    {{- end -}}
{{- end -}}

{{/*
Return the secret with new PostgreSQL credentials
*/}}
{{- define "postgresql.v1.update-job.newSecretName" -}}
    {{- if and (not .Values.passwordUpdateJob.previousPasswords.existingSecret) (not .Values.passwordUpdateJob.previousPasswords.postgresPassword) -}}
        {{- /* The secret with the new password is managed by the helm chart. We create a new secret as the current one has the old password */ -}}
        {{- printf "%s-new-secret" (include "postgresql.v1.chart.fullname" .) | trunc 63 | trimSuffix "-" -}}
    {{- else -}}
        {{- /* The secret with the new password is managed externally */ -}}
        {{- include "postgresql.v1.secretName" . -}}
    {{- end -}}
{{- end -}}

{{/*
Return PostgreSQL service port
*/}}
{{- define "postgresql.v1.service.port" -}}
{{- if .Values.global.postgresql.service.ports.postgresql -}}
    {{- .Values.global.postgresql.service.ports.postgresql -}}
{{- else -}}
    {{- .Values.primary.service.ports.postgresql -}}
{{- end -}}
{{- end -}}

{{/*
Return PostgreSQL service port
*/}}
{{- define "postgresql.v1.readReplica.service.port" -}}
{{- if .Values.global.postgresql.service.ports.postgresql -}}
    {{- .Values.global.postgresql.service.ports.postgresql -}}
{{- else -}}
    {{- .Values.readReplicas.service.ports.postgresql -}}
{{- end -}}
{{- end -}}

{{/*
Get the PostgreSQL primary configuration ConfigMap name.
*/}}
{{- define "postgresql.v1.primary.configmapName" -}}
{{- if .Values.primary.existingConfigmap -}}
    {{- printf "%s" (tpl .Values.primary.existingConfigmap $) -}}
{{- else -}}
    {{- printf "%s-configuration" (include "postgresql.v1.primary.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Return true if a configmap object should be created for PostgreSQL primary with the configuration
*/}}
{{- define "postgresql.v1.primary.createConfigmap" -}}
{{- if and (or .Values.primary.configuration .Values.primary.pgHbaConfiguration) (not .Values.primary.existingConfigmap) -}}
    {{- true -}}
{{- else -}}
{{- end -}}
{{- end -}}

{{/*
Get the PostgreSQL primary extended configuration ConfigMap name.
*/}}
{{- define "postgresql.v1.primary.extendedConfigmapName" -}}
{{- if .Values.primary.existingExtendedConfigmap -}}
    {{- printf "%s" (tpl .Values.primary.existingExtendedConfigmap $) -}}
{{- else -}}
    {{- printf "%s-extended-configuration" (include "postgresql.v1.primary.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Get the PostgreSQL read replica extended configuration ConfigMap name.
*/}}
{{- define "postgresql.v1.readReplicas.extendedConfigmapName" -}}
    {{- printf "%s-extended-configuration" (include "postgresql.v1.readReplica.fullname" .) -}}
{{- end -}}

{{/*
Return true if a configmap object should be created for PostgreSQL primary with the extended configuration
*/}}
{{- define "postgresql.v1.primary.createExtendedConfigmap" -}}
{{- if and .Values.primary.extendedConfiguration (not .Values.primary.existingExtendedConfigmap) -}}
    {{- true -}}
{{- else -}}
{{- end -}}
{{- end -}}

{{/*
Return true if a configmap object should be created for PostgreSQL read replica with the extended configuration
*/}}
{{- define "postgresql.v1.readReplicas.createExtendedConfigmap" -}}
{{- if .Values.readReplicas.extendedConfiguration -}}
    {{- true -}}
{{- else -}}
{{- end -}}
{{- end -}}

{{/*
 Create the name of the service account to use
 */}}
{{- define "postgresql.v1.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "postgresql.v1.chart.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Return true if a configmap should be mounted with PostgreSQL configuration
*/}}
{{- define "postgresql.v1.mountConfigurationCM" -}}
{{- if or .Values.primary.configuration .Values.primary.pgHbaConfiguration .Values.primary.existingConfigmap -}}
    {{- true -}}
{{- end -}}
{{- end -}}

{{/*
Get the pre-initialization scripts ConfigMap name.
*/}}
{{- define "postgresql.v1.preInitDb.scriptsCM" -}}
{{- if .Values.primary.preInitDb.scriptsConfigMap -}}
    {{- printf "%s" (tpl .Values.primary.preInitDb.scriptsConfigMap $) -}}
{{- else -}}
    {{- printf "%s-preinit-scripts" (include "postgresql.v1.primary.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Get the initialization scripts ConfigMap name.
*/}}
{{- define "postgresql.v1.initdb.scriptsCM" -}}
{{- if .Values.primary.initdb.scriptsConfigMap -}}
    {{- printf "%s" (tpl .Values.primary.initdb.scriptsConfigMap $) -}}
{{- else -}}
    {{- printf "%s-init-scripts" (include "postgresql.v1.primary.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Return true if TLS is enabled for LDAP connection
*/}}
{{- define "postgresql.v1.ldap.tls.enabled" -}}
{{- if and (kindIs "string" .Values.ldap.tls) (not (empty .Values.ldap.tls)) -}}
    {{- true -}}
{{- else if and (kindIs "map" .Values.ldap.tls) .Values.ldap.tls.enabled -}}
    {{- true -}}
{{- end -}}
{{- end -}}

{{/*
Get the readiness probe command
*/}}
{{- define "postgresql.v1.readinessProbeCommand" -}}
{{- $customUser := include "postgresql.v1.username" . -}}
- |
{{- if (include "postgresql.v1.database" .) }}
  exec pg_isready -U {{ default "postgres" $customUser | quote }} -d "dbname={{ include "postgresql.v1.database" . }} {{- if .Values.tls.enabled }} sslcert={{ include "postgresql.v1.tlsCert" . }} sslkey={{ include "postgresql.v1.tlsCertKey" . }}{{- end }}" -h 127.0.0.1 -p {{ .Values.containerPorts.postgresql }}
{{- else }}
  exec pg_isready -U {{ default "postgres" $customUser | quote }} {{- if .Values.tls.enabled }} -d "sslcert={{ include "postgresql.v1.tlsCert" . }} sslkey={{ include "postgresql.v1.tlsCertKey" . }}"{{- end }} -h 127.0.0.1 -p {{ .Values.containerPorts.postgresql }}
{{- end }}
  [ -f /opt/bitnami/postgresql/tmp/.initialized ] || [ -f /bitnami/postgresql/.initialized ]
{{- end -}}

{{/*
Compile all warnings into a single message, and call fail.
*/}}
{{- define "postgresql.v1.validateValues" -}}
{{- $messages := list -}}
{{- $messages := append $messages (include "postgresql.v1.validateValues.ldapConfigurationMethod" .) -}}
{{- $messages := append $messages (include "postgresql.v1.validateValues.psp" .) -}}
{{- $messages := without $messages "" -}}
{{- $message := join "\n" $messages -}}

{{- if $message -}}
{{- printf "\nVALUES VALIDATION:\n%s" $message | fail -}}
{{- end -}}
{{- end -}}

{{/*
Validate values of Postgresql - If ldap.url is used then you don't need the other settings for ldap
*/}}
{{- define "postgresql.v1.validateValues.ldapConfigurationMethod" -}}
{{- if and .Values.ldap.enabled (and (not (empty .Values.ldap.url)) (not (empty .Values.ldap.server))) -}}
postgresql: ldap.url, ldap.server
    You cannot set both `ldap.url` and `ldap.server` at the same time.
    Please provide a unique way to configure LDAP.
    More info at https://www.postgresql.org/docs/current/auth-ldap.html
{{- end -}}
{{- end -}}

{{/*
Validate values of Postgresql - If PSP is enabled RBAC should be enabled too
*/}}
{{- define "postgresql.v1.validateValues.psp" -}}
{{- if and .Values.psp.create (not .Values.rbac.create) -}}
postgresql: psp.create, rbac.create
    RBAC should be enabled if PSP is enabled in order for PSP to work.
    More info at https://kubernetes.io/docs/concepts/policy/pod-security-policy/#authorizing-policies
{{- end -}}
{{- end -}}

{{/*
Return the path to the cert file.
*/}}
{{- define "postgresql.v1.tlsCert" -}}
{{- if .Values.tls.autoGenerated -}}
    {{- printf "/opt/bitnami/postgresql/certs/tls.crt" -}}
{{- else -}}
    {{- required "Certificate filename is required when TLS in enabled" .Values.tls.certFilename | printf "/opt/bitnami/postgresql/certs/%s" -}}
{{- end -}}
{{- end -}}

{{/*
Return the path to the cert key file.
*/}}
{{- define "postgresql.v1.tlsCertKey" -}}
{{- if .Values.tls.autoGenerated -}}
    {{- printf "/opt/bitnami/postgresql/certs/tls.key" -}}
{{- else -}}
{{- required "Certificate Key filename is required when TLS in enabled" .Values.tls.certKeyFilename | printf "/opt/bitnami/postgresql/certs/%s" -}}
{{- end -}}
{{- end -}}

{{/*
Return the path to the CA cert file.
*/}}
{{- define "postgresql.v1.tlsCACert" -}}
{{- if .Values.tls.autoGenerated -}}
    {{- printf "/opt/bitnami/postgresql/certs/ca.crt" -}}
{{- else -}}
    {{- printf "/opt/bitnami/postgresql/certs/%s" .Values.tls.certCAFilename -}}
{{- end -}}
{{- end -}}

{{/*
Return the path to the CRL file.
*/}}
{{- define "postgresql.v1.tlsCRL" -}}
{{- if .Values.tls.crlFilename -}}
{{- printf "/opt/bitnami/postgresql/certs/%s" .Values.tls.crlFilename -}}
{{- end -}}
{{- end -}}

{{/*
Return true if a TLS credentials secret object should be created
*/}}
{{- define "postgresql.v1.createTlsSecret" -}}
{{- if and .Values.tls.autoGenerated (not .Values.tls.certificatesSecret) -}}
    {{- true -}}
{{- end -}}
{{- end -}}

{{/*
Return the path to the CA cert file.
*/}}
{{- define "postgresql.v1.tlsSecretName" -}}
{{- if .Values.tls.autoGenerated -}}
    {{- printf "%s-crt" (include "postgresql.v1.chart.fullname" .) -}}
{{- else -}}
    {{ tpl (required "A secret containing TLS certificates is required when TLS is enabled" .Values.tls.certificatesSecret) . }}
{{- end -}}
{{- end -}}

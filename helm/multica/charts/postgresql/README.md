<!--- app-name: PostgreSQL -->

# Bitnami package for PostgreSQL

PostgreSQL (Postgres) is an open source object-relational database known for reliability and data integrity. ACID-compliant, it supports foreign keys, joins, views, triggers and stored procedures.

[Overview of PostgreSQL](http://www.postgresql.org)

Trademarks: This software listing is packaged by Bitnami. The respective trademarks mentioned in the offering are owned by the respective companies, and use of them does not imply any affiliation or endorsement.

## TL;DR

```console
helm install my-release oci://registry-1.docker.io/bitnamicharts/postgresql
```

> Tip: Did you know that this app is also available as a Kubernetes App on the Azure Marketplace? Kubernetes Apps are the easiest way to deploy Bitnami on AKS. Click [here](https://azuremarketplace.microsoft.com/en-us/marketplace/apps/bitnami.postgresql-cnab) to see the listing on Azure Marketplace.

Looking to use PostgreSQL in production? Try [VMware Tanzu Application Catalog](https://bitnami.com/enterprise), the commercial edition of the Bitnami catalog.

## ⚠️ Important Notice: Upcoming changes to the Bitnami Catalog

Beginning August 28th, 2025, Bitnami will evolve its public catalog to offer a curated set of hardened, security-focused images under the new [Bitnami Secure Images initiative](https://news.broadcom.com/app-dev/broadcom-introduces-bitnami-secure-images-for-production-ready-containerized-applications). As part of this transition:

- Granting community users access for the first time to security-optimized versions of popular container images.
- Bitnami will begin deprecating support for non-hardened, Debian-based software images in its free tier and will gradually remove non-latest tags from the public catalog. As a result, community users will have access to a reduced number of hardened images. These images are published only under the “latest” tag and are intended for development purposes
- Starting August 28th, over two weeks, all existing container images, including older or versioned tags (e.g., 2.50.0, 10.6), will be migrated from the public catalog (docker.io/bitnami) to the “Bitnami Legacy” repository (docker.io/bitnamilegacy), where they will no longer receive updates.
- For production workloads and long-term support, users are encouraged to adopt Bitnami Secure Images, which include hardened containers, smaller attack surfaces, CVE transparency (via VEX/KEV), SBOMs, and enterprise support.

These changes aim to improve the security posture of all Bitnami users by promoting best practices for software supply chain integrity and up-to-date deployments. For more details, visit the [Bitnami Secure Images announcement](https://github.com/bitnami/containers/issues/83267).

## Introduction

This chart bootstraps a [PostgreSQL](https://github.com/bitnami/containers/tree/main/bitnami/postgresql) deployment on a [Kubernetes](https://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

For HA, please see [this repo](https://github.com/bitnami/charts/tree/main/bitnami/postgresql-ha)

## Prerequisites

- Kubernetes 1.23+
- Helm 3.8.0+
- PV provisioner support in the underlying infrastructure

## Installing the Chart

To install the chart with the release name `my-release`:

```console
helm install my-release oci://REGISTRY_NAME/REPOSITORY_NAME/postgresql
```

> Note: You need to substitute the placeholders `REGISTRY_NAME` and `REPOSITORY_NAME` with a reference to your Helm chart registry and repository. For example, in the case of Bitnami, you need to use `REGISTRY_NAME=registry-1.docker.io` and `REPOSITORY_NAME=bitnamicharts`.

The command deploys PostgreSQL on the Kubernetes cluster in the default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

> **Tip**: List all releases using `helm list`

## Configuration and installation details

### Resource requests and limits

Bitnami charts allow setting resource requests and limits for all containers inside the chart deployment. These are inside the `resources` value (check parameter table). Setting requests is essential for production workloads and these should be adapted to your specific use case.

To make this process easier, the chart contains the `resourcesPreset` values, which automatically sets the `resources` section according to different presets. Check these presets in [the bitnami/common chart](https://github.com/bitnami/charts/blob/main/bitnami/common/templates/_resources.tpl#L15). However, in production workloads using `resourcesPreset` is discouraged as it may not fully adapt to your specific needs. Find more information on container resource management in the [official Kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/).

### Prometheus metrics

This chart can be integrated with Prometheus by setting `metrics.enabled` to `true`. This will deploy a sidecar container with [postgres_exporter](https://github.com/prometheus-community/postgres_exporter) in all pods. It will also create `metrics` services that can be configured under the `metrics.service` section. These services will be have the necessary annotations to be automatically scraped by Prometheus.

#### Prometheus requirements

It is necessary to have a working installation of Prometheus or Prometheus Operator for the integration to work. Install the [Bitnami Prometheus helm chart](https://github.com/bitnami/charts/tree/main/bitnami/prometheus) or the [Bitnami Kube Prometheus helm chart](https://github.com/bitnami/charts/tree/main/bitnami/kube-prometheus) to easily have a working Prometheus in your cluster.

#### Integration with Prometheus Operator

The chart can deploy `ServiceMonitor` objects for integration with Prometheus Operator installations. To do so, set the value `metrics.serviceMonitor.enabled=true`. Ensure that the Prometheus Operator `CustomResourceDefinitions` are installed in the cluster or it will fail with the following error:

```text
no matches for kind "ServiceMonitor" in version "monitoring.coreos.com/v1"
```

Install the [Bitnami Kube Prometheus helm chart](https://github.com/bitnami/charts/tree/main/bitnami/kube-prometheus) for having the necessary CRDs and the Prometheus Operator.

### [Rolling VS Immutable tags](https://techdocs.broadcom.com/us/en/vmware-tanzu/application-catalog/tanzu-application-catalog/services/tac-doc/apps-tutorials-understand-rolling-tags-containers-index.html)

It is strongly recommended to use immutable tags in a production environment. This ensures your deployment does not change automatically if the same tag is updated with a different image.

Bitnami will release a new chart updating its containers if a new version of the main container, significant changes, or critical vulnerabilities exist.

### Customizing primary and read replica services in a replicated configuration

At the top level, there is a service object which defines the services for both primary and readReplicas. For deeper customization, there are service objects for both the primary and read types individually. This allows you to override the values in the top level service object so that the primary and read can be of different service types and with different clusterIPs / nodePorts. Also in the case you want the primary and read to be of type nodePort, you will need to set the nodePorts to different values to prevent a collision. The values that are deeper in the primary.service or readReplicas.service objects will take precedence over the top level service object.

### Use a different PostgreSQL version

To modify the application version used in this chart, specify a different version of the image using the `image.tag` parameter and/or a different repository using the `image.repository` parameter.

### LDAP

LDAP support can be enabled in the chart by specifying the `ldap.` parameters while creating a release. The following parameters should be configured to properly enable the LDAP support in the chart.

- **ldap.enabled**: Enable LDAP support. Defaults to `false`.
- **ldap.uri**: LDAP URL beginning in the form `ldap[s]://<hostname>:<port>`. No defaults.
- **ldap.base**: LDAP base DN. No defaults.
- **ldap.binddn**: LDAP bind DN. No defaults.
- **ldap.bindpw**: LDAP bind password. No defaults.
- **ldap.bslookup**: LDAP base lookup. No defaults.
- **ldap.nss_initgroups_ignoreusers**: LDAP ignored users. `root,nslcd`.
- **ldap.scope**: LDAP search scope. No defaults.
- **ldap.tls_reqcert**: LDAP TLS check on server certificates. No defaults.

For example:

```text
ldap.enabled="true"
ldap.uri="ldap://my_ldap_server"
ldap.base="dc=example\,dc=org"
ldap.binddn="cn=admin\,dc=example\,dc=org"
ldap.bindpw="admin"
ldap.bslookup="ou=group-ok\,dc=example\,dc=org"
ldap.nss_initgroups_ignoreusers="root\,nslcd"
ldap.scope="sub"
ldap.tls_reqcert="demand"
```

Next, login to the PostgreSQL server using the `psql` client and add the PAM authenticated LDAP users.

> Note: Parameters including commas must be escaped as shown in the above example.

### Update credentials

Bitnami charts, with its default settings, configure credentials at first boot. Any further change in the secrets or credentials can be done using one of the following methods:

### Manual update of the passwords and secrets

- Update the user password following [the upstream documentation](https://www.postgresql.org/docs/current/sql-alteruser.html)
- Update the password secret with the new values (replace the SECRET_NAME, PASSWORD and POSTGRES_PASSWORD placeholders)

```shell
kubectl create secret generic SECRET_NAME --from-literal=password=PASSWORD --from-literal=postgres-password=POSTGRES_PASSWORD --dry-run -o yaml | kubectl apply -f -
```

### Automated update using a password update job

The Bitnami PostgreSQL provides a password update job that will automatically change the PostgreSQL passwords when running helm upgrade. To enable the job set `passwordUpdateJob.enabled=true`. This job requires:

- The new passwords: this is configured using either `auth.postgresPassword`, `auth.password` and `auth.replicationPassword` (if applicable) or setting `auth.existingSecret`.
- The previous passwords: This value is taken automatically from already deployed secret object. If you are using `auth.existingSecret` or `helm template` instead of `helm upgrade`, then set either `passwordUpdateJob.previousPasswords.postgresPassword`, `passwordUpdateJob.previousPasswords.password`, `passwordUpdateJob.previousPasswords.replicationPassword` (when applicable), or setting `passwordUpdateJob.previousPasswords.existingSecret`.

In the following example we update the password via values.yaml in a PostgreSQL installation with replication

```yaml
architecture: "replication"

auth:
  user: "user"
  postgresPassword: "newPostgresPassword123"
  password: "newUserPassword123"
  replicationPassword: "newReplicationPassword123"

passwordUpdateJob:
  enabled: true
```

In this example we use two existing secrets (`new-password-secret` and `previous-password-secret`) to update the passwords:

```yaml
auth:
  existingSecret: new-password-secret

passwordUpdateJob:
  enabled: true
  previousPasswords:
    existingSecret: previous-password-secret
```

You can add extra update commands using the `passwordUpdateJob.extraCommands` value.

### postgresql.conf / pg_hba.conf files as configMap

This helm chart also supports to customize the PostgreSQL configuration file. You can add additional PostgreSQL configuration parameters using the `primary.extendedConfiguration`/`readReplicas.extendedConfiguration` parameters as a string. Alternatively, to replace the entire default configuration use `primary.configuration`.

You can also add a custom pg_hba.conf using the `primary.pgHbaConfiguration` parameter.

In addition to these options, you can also set an external ConfigMap with all the configuration files. This is done by setting the `primary.existingConfigmap` parameter. Note that this will override the two previous options.

### Initialize a fresh instance

The [Bitnami PostgreSQL](https://github.com/bitnami/containers/tree/main/bitnami/postgresql) image allows you to use your custom scripts to initialize a fresh instance. In order to execute the scripts, you can specify custom scripts using the `primary.initdb.scripts` parameter as a string.

In addition, you can also set an external ConfigMap with all the initialization scripts. This is done by setting the `primary.initdb.scriptsConfigMap` parameter. Note that this will override the two previous options. If your initialization scripts contain sensitive information such as credentials or passwords, you can use the `primary.initdb.scriptsSecret` parameter.

The allowed extensions are `.sh`, `.sql` and `.sql.gz`.

### Securing traffic using TLS

TLS support can be enabled in the chart by specifying the `tls.` parameters while creating a release. The following parameters should be configured to properly enable the TLS support in the chart:

- `tls.enabled`: Enable TLS support. Defaults to `false`
- `tls.certificatesSecret`: Name of an existing secret that contains the certificates. No defaults.
- `tls.certFilename`: Certificate filename. No defaults.
- `tls.certKeyFilename`: Certificate key filename. No defaults.

For example:

- First, create the secret with the cetificates files:

    ```console
    kubectl create secret generic certificates-tls-secret --from-file=./cert.crt --from-file=./cert.key --from-file=./ca.crt
    ```

- Then, use the following parameters:

    ```console
    volumePermissions.enabled=true
    tls.enabled=true
    tls.certificatesSecret="certificates-tls-secret"
    tls.certFilename="cert.crt"
    tls.certKeyFilename="cert.key"
    ```

  > Note TLS and VolumePermissions: PostgreSQL requires certain permissions on sensitive files (such as certificate keys) to start up. Due to an on-going [issue](https://github.com/kubernetes/kubernetes/issues/57923) regarding kubernetes permissions and the use of `containerSecurityContext.runAsUser`, you must enable `volumePermissions` to ensure everything works as expected.

### Sidecars

If you need  additional containers to run within the same pod as PostgreSQL (e.g. an additional metrics or logging exporter), you can do so via the `sidecars` config parameter. Simply define your container according to the Kubernetes container spec.

```yaml
# For the PostgreSQL primary
primary:
  sidecars:
  - name: your-image-name
    image: your-image
    imagePullPolicy: Always
    ports:
    - name: portname
     containerPort: 1234
# For the PostgreSQL replicas
readReplicas:
  sidecars:
  - name: your-image-name
    image: your-image
    imagePullPolicy: Always
    ports:
    - name: portname
     containerPort: 1234
```

### Metrics

The chart optionally can start a metrics exporter for [prometheus](https://prometheus.io). The metrics endpoint (port 9187) is not exposed and it is expected that the metrics are collected from inside the k8s cluster using something similar as the described in the [example Prometheus scrape configuration](https://github.com/prometheus/prometheus/blob/master/documentation/examples/prometheus-kubernetes.yml).

The exporter allows to create custom metrics from additional SQL queries. See the Chart's `values.yaml` for an example and consult the [exporters documentation](https://github.com/wrouesnel/postgres_exporter#adding-new-metrics-via-a-config-file) for more details.

### Use of global variables

In more complex scenarios, we may have the following tree of dependencies

```text
                     +--------------+
                     |              |
        +------------+   Chart 1    +-----------+
        |            |              |           |
        |            --------+------+           |
        |                    |                  |
        |                    |                  |
        |                    |                  |
        |                    |                  |
        v                    v                  v
+-------+------+    +--------+------+  +--------+------+
|              |    |               |  |               |
|  PostgreSQL  |    |  Sub-chart 1  |  |  Sub-chart 2  |
|              |    |               |  |               |
+--------------+    +---------------+  +---------------+
```

The three charts below depend on the parent chart Chart 1. However, subcharts 1 and 2 may need to connect to PostgreSQL as well. In order to do so, subcharts 1 and 2 need to know the PostgreSQL credentials, so one option for deploying could be deploy Chart 1 with the following parameters:

```text
postgresql.auth.username=testuser
subchart1.postgresql.auth.username=testuser
subchart2.postgresql.auth.username=testuser
postgresql.auth.password=testpass
subchart1.postgresql.auth.password=testpass
subchart2.postgresql.auth.password=testpass
postgresql.auth.database=testdb
subchart1.postgresql.auth.database=testdb
subchart2.postgresql.auth.database=testdb
```

If the number of dependent sub-charts increases, installing the chart with parameters can become increasingly difficult. An alternative would be to set the credentials using global variables as follows:

```text
global.postgresql.auth.username=testuser
global.postgresql.auth.password=testpass
global.postgresql.auth.database=testdb
```

This way, the credentials will be available in all of the subcharts.

### Backup and restore

To back up and restore Bitnami PostgreSQL Helm chart deployments on Kubernetes, you need to back up the persistent volumes from the source deployment and attach them to a new deployment using [Velero](https://velero.io/), a Kubernetes backup/restore tool.

These are the steps you will usually follow to back up and restore your PostgreSQL cluster data:

- Install Velero on the source and destination clusters.
- Use Velero to back up the PersistentVolumes (PVs) used by the deployment on the source cluster.
- Use Velero to restore the backed-up PVs on the destination cluster.
- Create a new deployment on the destination cluster with the same chart, deployment name, credentials and other parameters as the original. This new deployment will use the restored PVs and hence the original data.

Refer to our detailed [tutorial on backing up and restoring PostgreSQL deployments on Kubernetes](https://techdocs.broadcom.com/us/en/vmware-tanzu/application-catalog/tanzu-application-catalog/services/tac-doc/apps-tutorials-migrate-data-tac-velero-index.html) for more information.

### NetworkPolicy

To enable network policy for PostgreSQL, install [a networking plugin that implements the Kubernetes NetworkPolicy spec](https://kubernetes.io/docs/tasks/administer-cluster/declare-network-policy#before-you-begin), and set `networkPolicy.enabled` to `true`.

For Kubernetes v1.5 & v1.6, you must also turn on NetworkPolicy by setting the DefaultDeny namespace annotation. Note: this will enforce policy for _all_ pods in the namespace:

```console
kubectl annotate namespace default "net.beta.kubernetes.io/network-policy={\"ingress\":{\"isolation\":\"DefaultDeny\"}}"
```

With NetworkPolicy enabled, traffic will be limited to just port 5432.

For more precise policy, set `networkPolicy.allowExternal=false`. This will only allow pods with the generated client label to connect to PostgreSQL.
This label will be displayed in the output of a successful install.

### Differences between Bitnami PostgreSQL image and [Docker Official](https://hub.docker.com/_/postgres) image

- The Docker Official PostgreSQL image does not support replication. If you pass any replication environment variable, this would be ignored. The only environment variables supported by the Docker Official image are POSTGRES_USER, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_INITDB_ARGS, POSTGRES_INITDB_WALDIR and PGDATA. All the remaining environment variables are specific to the Bitnami PostgreSQL image.
- The Bitnami PostgreSQL image is non-root by default. This requires that you run the pod with `securityContext` and updates the permissions of the volume with an `initContainer`. A key benefit of this configuration is that the pod follows security best practices and is prepared to run on Kubernetes distributions with hard security constraints like OpenShift.
- For OpenShift up to 4.10, let set the volume permissions, security context, runAsUser and fsGroup automatically by OpenShift and disable the predefined settings of the helm chart: primary.securityContext.enabled=false,primary.containerSecurityContext.enabled=false,volumePermissions.enabled=false,shmVolume.enabled=false
- For OpenShift 4.11 and higher, let set OpenShift the runAsUser and fsGroup automatically. Configure the pod and container security context to restrictive defaults and disable the volume permissions setup: primary.
    podSecurityContext.fsGroup=null,primary.podSecurityContext.seccompProfile.type=RuntimeDefault,primary.containerSecurityContext.runAsUser=null,primary.containerSecurityContext.allowPrivilegeEscalation=false,primary.containerSecurityContext.runAsNonRoot=true,primary.containerSecurityContext.seccompProfile.type=RuntimeDefault,primary.containerSecurityContext.capabilities.drop=['ALL'],volumePermissions.enabled=false,shmVolume.enabled=false

### Setting Pod's affinity

This chart allows you to set your custom affinity using the `XXX.affinity` parameter(s). Find more information about Pod's affinity in the [kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity).

As an alternative, you can use of the preset configurations for pod affinity, pod anti-affinity, and node affinity available at the [bitnami/common](https://github.com/bitnami/charts/tree/main/bitnami/common#affinities) chart. To do so, set the `XXX.podAffinityPreset`, `XXX.podAntiAffinityPreset`, or `XXX.nodeAffinityPreset` parameters.

## Persistence

The [Bitnami PostgreSQL](https://github.com/bitnami/containers/tree/main/bitnami/postgresql) image stores the PostgreSQL data and configurations at the `/bitnami/postgresql` path of the container.

Persistent Volume Claims are used to keep the data across deployments. This is known to work in GCE, AWS, and minikube.
See the [Parameters](#parameters) section to configure the PVC or to disable persistence.

If you already have data in it, you will fail to sync to standby nodes for all commits, details can refer to the [code present in the container repository](https://github.com/bitnami/containers/tree/main/bitnami/postgresql). If you need to use those data, please covert them to sql and import after `helm install` finished.

## Parameters

### Global parameters

| Name                                                       | Description                                                                                                                                                                                                                                                                                                                                                         | Value   |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `global.imageRegistry`                                     | Global Docker image registry                                                                                                                                                                                                                                                                                                                                        | `""`    |
| `global.imagePullSecrets`                                  | Global Docker registry secret names as an array                                                                                                                                                                                                                                                                                                                     | `[]`    |
| `global.defaultStorageClass`                               | Global default StorageClass for Persistent Volume(s)                                                                                                                                                                                                                                                                                                                | `""`    |
| `global.storageClass`                                      | DEPRECATED: use global.defaultStorageClass instead                                                                                                                                                                                                                                                                                                                  | `""`    |
| `global.security.allowInsecureImages`                      | Allows skipping image verification                                                                                                                                                                                                                                                                                                                                  | `false` |
| `global.postgresql.fullnameOverride`                       | Full chart name (overrides `fullnameOverride`)                                                                                                                                                                                                                                                                                                                      | `""`    |
| `global.postgresql.auth.postgresPassword`                  | Password for the "postgres" admin user (overrides `auth.postgresPassword`)                                                                                                                                                                                                                                                                                          | `""`    |
| `global.postgresql.auth.username`                          | Name for a custom user to create (overrides `auth.username`)                                                                                                                                                                                                                                                                                                        | `""`    |
| `global.postgresql.auth.password`                          | Password for the custom user to create (overrides `auth.password`)                                                                                                                                                                                                                                                                                                  | `""`    |
| `global.postgresql.auth.database`                          | Name for a custom database to create (overrides `auth.database`)                                                                                                                                                                                                                                                                                                    | `""`    |
| `global.postgresql.auth.existingSecret`                    | Name of existing secret to use for PostgreSQL credentials (overrides `auth.existingSecret`).                                                                                                                                                                                                                                                                        | `""`    |
| `global.postgresql.auth.secretKeys.adminPasswordKey`       | Name of key in existing secret to use for PostgreSQL credentials (overrides `auth.secretKeys.adminPasswordKey`). Only used when `global.postgresql.auth.existingSecret` is set.                                                                                                                                                                                     | `""`    |
| `global.postgresql.auth.secretKeys.userPasswordKey`        | Name of key in existing secret to use for PostgreSQL credentials (overrides `auth.secretKeys.userPasswordKey`). Only used when `global.postgresql.auth.existingSecret` is set.                                                                                                                                                                                      | `""`    |
| `global.postgresql.auth.secretKeys.replicationPasswordKey` | Name of key in existing secret to use for PostgreSQL credentials (overrides `auth.secretKeys.replicationPasswordKey`). Only used when `global.postgresql.auth.existingSecret` is set.                                                                                                                                                                               | `""`    |
| `global.postgresql.service.ports.postgresql`               | PostgreSQL service port (overrides `service.ports.postgresql`)                                                                                                                                                                                                                                                                                                      | `""`    |
| `global.compatibility.openshift.adaptSecurityContext`      | Adapt the securityContext sections of the deployment to make them compatible with Openshift restricted-v2 SCC: remove runAsUser, runAsGroup and fsGroup and let the platform use their allowed default IDs. Possible values: auto (apply if the detected running cluster is Openshift), force (perform the adaptation always), disabled (do not perform adaptation) | `auto`  |

### Common parameters

| Name                     | Description                                                                                  | Value           |
| ------------------------ | -------------------------------------------------------------------------------------------- | --------------- |
| `kubeVersion`            | Override Kubernetes version                                                                  | `""`            |
| `nameOverride`           | String to partially override common.names.fullname template (will maintain the release name) | `""`            |
| `fullnameOverride`       | String to fully override common.names.fullname template                                      | `""`            |
| `namespaceOverride`      | String to fully override common.names.namespace                                              | `""`            |
| `clusterDomain`          | Kubernetes Cluster Domain                                                                    | `cluster.local` |
| `extraDeploy`            | Array of extra objects to deploy with the release (evaluated as a template)                  | `[]`            |
| `commonLabels`           | Add labels to all the deployed resources                                                     | `{}`            |
| `commonAnnotations`      | Add annotations to all the deployed resources                                                | `{}`            |
| `secretAnnotations`      | Add annotations to the secrets                                                               | `{}`            |
| `diagnosticMode.enabled` | Enable diagnostic mode (all probes will be disabled and the command will be overridden)      | `false`         |
| `diagnosticMode.command` | Command to override all containers in the statefulset                                        | `["sleep"]`     |
| `diagnosticMode.args`    | Args to override all containers in the statefulset                                           | `["infinity"]`  |

### PostgreSQL common parameters

| Name                                     | Description                                                                                                                                                                                                                                                                                                                                   | Value                        |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| `image.registry`                         | PostgreSQL image registry                                                                                                                                                                                                                                                                                                                     | `REGISTRY_NAME`              |
| `image.repository`                       | PostgreSQL image repository                                                                                                                                                                                                                                                                                                                   | `REPOSITORY_NAME/postgresql` |
| `image.digest`                           | PostgreSQL image digest in the way sha256:aa.... Please note this parameter, if set, will override the tag                                                                                                                                                                                                                                    | `""`                         |
| `image.pullPolicy`                       | PostgreSQL image pull policy                                                                                                                                                                                                                                                                                                                  | `IfNotPresent`               |
| `image.pullSecrets`                      | Specify image pull secrets                                                                                                                                                                                                                                                                                                                    | `[]`                         |
| `image.debug`                            | Specify if debug values should be set                                                                                                                                                                                                                                                                                                         | `false`                      |
| `auth.enablePostgresUser`                | Assign a password to the "postgres" admin user. Otherwise, remote access will be blocked for this user                                                                                                                                                                                                                                        | `true`                       |
| `auth.postgresPassword`                  | Password for the "postgres" admin user. Ignored if `auth.existingSecret` is provided                                                                                                                                                                                                                                                          | `""`                         |
| `auth.username`                          | Name for a custom user to create                                                                                                                                                                                                                                                                                                              | `""`                         |
| `auth.password`                          | Password for the custom user to create. Ignored if `auth.existingSecret` is provided                                                                                                                                                                                                                                                          | `""`                         |
| `auth.database`                          | Name for a custom database to create                                                                                                                                                                                                                                                                                                          | `""`                         |
| `auth.replicationUsername`               | Name of the replication user                                                                                                                                                                                                                                                                                                                  | `repl_user`                  |
| `auth.replicationPassword`               | Password for the replication user. Ignored if `auth.existingSecret` is provided                                                                                                                                                                                                                                                               | `""`                         |
| `auth.existingSecret`                    | Name of existing secret to use for PostgreSQL credentials. `auth.postgresPassword`, `auth.password`, and `auth.replicationPassword` will be ignored and picked up from this secret. The secret might also contains the key `ldap-password` if LDAP is enabled. `ldap.bind_password` will be ignored and picked from this secret in this case. | `""`                         |
| `auth.secretKeys.adminPasswordKey`       | Name of key in existing secret to use for PostgreSQL credentials. Only used when `auth.existingSecret` is set.                                                                                                                                                                                                                                | `postgres-password`          |
| `auth.secretKeys.userPasswordKey`        | Name of key in existing secret to use for PostgreSQL credentials. Only used when `auth.existingSecret` is set.                                                                                                                                                                                                                                | `password`                   |
| `auth.secretKeys.replicationPasswordKey` | Name of key in existing secret to use for PostgreSQL credentials. Only used when `auth.existingSecret` is set.                                                                                                                                                                                                                                | `replication-password`       |
| `auth.usePasswordFiles`                  | Mount credentials as a files instead of using an environment variable                                                                                                                                                                                                                                                                         | `true`                       |
| `architecture`                           | PostgreSQL architecture (`standalone` or `replication`)                                                                                                                                                                                                                                                                                       | `standalone`                 |
| `replication.synchronousCommit`          | Set synchronous commit mode. Allowed values: `on`, `remote_apply`, `remote_write`, `local` and `off`                                                                                                                                                                                                                                          | `off`                        |
| `replication.numSynchronousReplicas`     | Number of replicas that will have synchronous replication. Note: Cannot be greater than `readReplicas.replicaCount`.                                                                                                                                                                                                                          | `0`                          |
| `replication.applicationName`            | Cluster application name. Useful for advanced replication settings                                                                                                                                                                                                                                                                            | `my_application`             |
| `containerPorts.postgresql`              | PostgreSQL container port                                                                                                                                                                                                                                                                                                                     | `5432`                       |
| `audit.logHostname`                      | Log client hostnames                                                                                                                                                                                                                                                                                                                          | `false`                      |
| `audit.logConnections`                   | Add client log-in operations to the log file                                                                                                                                                                                                                                                                                                  | `false`                      |
| `audit.logDisconnections`                | Add client log-outs operations to the log file                                                                                                                                                                                                                                                                                                | `false`                      |
| `audit.pgAuditLog`                       | Add operations to log using the pgAudit extension                                                                                                                                                                                                                                                                                             | `""`                         |
| `audit.pgAuditLogCatalog`                | Log catalog using pgAudit                                                                                                                                                                                                                                                                                                                     | `off`                        |
| `audit.clientMinMessages`                | Message log level to share with the user                                                                                                                                                                                                                                                                                                      | `error`                      |
| `audit.logLinePrefix`                    | Template for log line prefix (default if not set)                                                                                                                                                                                                                                                                                             | `""`                         |
| `audit.logTimezone`                      | Timezone for the log timestamps                                                                                                                                                                                                                                                                                                               | `""`                         |
| `ldap.enabled`                           | Enable LDAP support                                                                                                                                                                                                                                                                                                                           | `false`                      |
| `ldap.server`                            | IP address or name of the LDAP server.                                                                                                                                                                                                                                                                                                        | `""`                         |
| `ldap.port`                              | Port number on the LDAP server to connect to                                                                                                                                                                                                                                                                                                  | `""`                         |
| `ldap.prefix`                            | String to prepend to the user name when forming the DN to bind                                                                                                                                                                                                                                                                                | `""`                         |
| `ldap.suffix`                            | String to append to the user name when forming the DN to bind                                                                                                                                                                                                                                                                                 | `""`                         |
| `ldap.basedn`                            | Root DN to begin the search for the user in                                                                                                                                                                                                                                                                                                   | `""`                         |
| `ldap.binddn`                            | DN of user to bind to LDAP                                                                                                                                                                                                                                                                                                                    | `""`                         |
| `ldap.bindpw`                            | Password for the user to bind to LDAP                                                                                                                                                                                                                                                                                                         | `""`                         |
| `ldap.searchAttribute`                   | Attribute to match against the user name in the search                                                                                                                                                                                                                                                                                        | `""`                         |
| `ldap.searchFilter`                      | The search filter to use when doing search+bind authentication                                                                                                                                                                                                                                                                                | `""`                         |
| `ldap.scheme`                            | Set to `ldaps` to use LDAPS                                                                                                                                                                                                                                                                                                                   | `""`                         |
| `ldap.tls.enabled`                       | Se to true to enable TLS encryption                                                                                                                                                                                                                                                                                                           | `false`                      |
| `ldap.uri`                               | LDAP URL beginning in the form `ldap[s]://host[:port]/basedn`. If provided, all the other LDAP parameters will be ignored.                                                                                                                                                                                                                    | `""`                         |
| `postgresqlDataDir`                      | PostgreSQL data dir folder                                                                                                                                                                                                                                                                                                                    | `/bitnami/postgresql/data`   |
| `postgresqlSharedPreloadLibraries`       | Shared preload libraries (comma-separated list)                                                                                                                                                                                                                                                                                               | `pgaudit`                    |
| `shmVolume.enabled`                      | Enable emptyDir volume for /dev/shm for PostgreSQL pod(s)                                                                                                                                                                                                                                                                                     | `true`                       |
| `shmVolume.sizeLimit`                    | Set this to enable a size limit on the shm tmpfs                                                                                                                                                                                                                                                                                              | `""`                         |
| `tls.enabled`                            | Enable TLS traffic support                                                                                                                                                                                                                                                                                                                    | `false`                      |
| `tls.autoGenerated`                      | Generate automatically self-signed TLS certificates                                                                                                                                                                                                                                                                                           | `false`                      |
| `tls.preferServerCiphers`                | Whether to use the server's TLS cipher preferences rather than the client's                                                                                                                                                                                                                                                                   | `true`                       |
| `tls.certificatesSecret`                 | Name of an existing secret that contains the certificates                                                                                                                                                                                                                                                                                     | `""`                         |
| `tls.certFilename`                       | Certificate filename                                                                                                                                                                                                                                                                                                                          | `""`                         |
| `tls.certKeyFilename`                    | Certificate key filename                                                                                                                                                                                                                                                                                                                      | `""`                         |
| `tls.certCAFilename`                     | CA Certificate filename                                                                                                                                                                                                                                                                                                                       | `""`                         |
| `tls.crlFilename`                        | File containing a Certificate Revocation List                                                                                                                                                                                                                                                                                                 | `""`                         |

### PostgreSQL Primary parameters

| Name                                                        | Description                                                                                                                                                                                                                       | Value                 |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| `primary.name`                                              | Name of the primary database (eg primary, master, leader, ...)                                                                                                                                                                    | `primary`             |
| `primary.configuration`                                     | PostgreSQL Primary main configuration to be injected as ConfigMap                                                                                                                                                                 | `""`                  |
| `primary.pgHbaConfiguration`                                | PostgreSQL Primary client authentication configuration                                                                                                                                                                            | `""`                  |
| `primary.existingConfigmap`                                 | Name of an existing ConfigMap with PostgreSQL Primary configuration                                                                                                                                                               | `""`                  |
| `primary.extendedConfiguration`                             | Extended PostgreSQL Primary configuration (appended to main or default configuration)                                                                                                                                             | `""`                  |
| `primary.existingExtendedConfigmap`                         | Name of an existing ConfigMap with PostgreSQL Primary extended configuration                                                                                                                                                      | `""`                  |
| `primary.initdb.args`                                       | PostgreSQL initdb extra arguments                                                                                                                                                                                                 | `""`                  |
| `primary.initdb.postgresqlWalDir`                           | Specify a custom location for the PostgreSQL transaction log                                                                                                                                                                      | `""`                  |
| `primary.initdb.scripts`                                    | Dictionary of initdb scripts                                                                                                                                                                                                      | `{}`                  |
| `primary.initdb.scriptsConfigMap`                           | ConfigMap with scripts to be run at first boot                                                                                                                                                                                    | `""`                  |
| `primary.initdb.scriptsSecret`                              | Secret with scripts to be run at first boot (in case it contains sensitive information)                                                                                                                                           | `""`                  |
| `primary.initdb.user`                                       | Specify the PostgreSQL username to execute the initdb scripts                                                                                                                                                                     | `""`                  |
| `primary.initdb.password`                                   | Specify the PostgreSQL password to execute the initdb scripts                                                                                                                                                                     | `""`                  |
| `primary.preInitDb.scripts`                                 | Dictionary of pre-init scripts                                                                                                                                                                                                    | `{}`                  |
| `primary.preInitDb.scriptsConfigMap`                        | ConfigMap with pre-init scripts to be run                                                                                                                                                                                         | `""`                  |
| `primary.preInitDb.scriptsSecret`                           | Secret with pre-init scripts to be run                                                                                                                                                                                            | `""`                  |
| `primary.standby.enabled`                                   | Whether to enable current cluster's primary as standby server of another cluster or not                                                                                                                                           | `false`               |
| `primary.standby.primaryHost`                               | The Host of replication primary in the other cluster                                                                                                                                                                              | `""`                  |
| `primary.standby.primaryPort`                               | The Port of replication primary in the other cluster                                                                                                                                                                              | `""`                  |
| `primary.extraEnvVars`                                      | Array with extra environment variables to add to PostgreSQL Primary nodes                                                                                                                                                         | `[]`                  |
| `primary.extraEnvVarsCM`                                    | Name of existing ConfigMap containing extra env vars for PostgreSQL Primary nodes                                                                                                                                                 | `""`                  |
| `primary.extraEnvVarsSecret`                                | Name of existing Secret containing extra env vars for PostgreSQL Primary nodes                                                                                                                                                    | `""`                  |
| `primary.command`                                           | Override default container command (useful when using custom images)                                                                                                                                                              | `[]`                  |
| `primary.args`                                              | Override default container args (useful when using custom images)                                                                                                                                                                 | `[]`                  |
| `primary.livenessProbe.enabled`                             | Enable livenessProbe on PostgreSQL Primary containers                                                                                                                                                                             | `true`                |
| `primary.livenessProbe.initialDelaySeconds`                 | Initial delay seconds for livenessProbe                                                                                                                                                                                           | `30`                  |
| `primary.livenessProbe.periodSeconds`                       | Period seconds for livenessProbe                                                                                                                                                                                                  | `10`                  |
| `primary.livenessProbe.timeoutSeconds`                      | Timeout seconds for livenessProbe                                                                                                                                                                                                 | `5`                   |
| `primary.livenessProbe.failureThreshold`                    | Failure threshold for livenessProbe                                                                                                                                                                                               | `6`                   |
| `primary.livenessProbe.successThreshold`                    | Success threshold for livenessProbe                                                                                                                                                                                               | `1`                   |
| `primary.readinessProbe.enabled`                            | Enable readinessProbe on PostgreSQL Primary containers                                                                                                                                                                            | `true`                |
| `primary.readinessProbe.initialDelaySeconds`                | Initial delay seconds for readinessProbe                                                                                                                                                                                          | `5`                   |
| `primary.readinessProbe.periodSeconds`                      | Period seconds for readinessProbe                                                                                                                                                                                                 | `10`                  |
| `primary.readinessProbe.timeoutSeconds`                     | Timeout seconds for readinessProbe                                                                                                                                                                                                | `5`                   |
| `primary.readinessProbe.failureThreshold`                   | Failure threshold for readinessProbe                                                                                                                                                                                              | `6`                   |
| `primary.readinessProbe.successThreshold`                   | Success threshold for readinessProbe                                                                                                                                                                                              | `1`                   |
| `primary.startupProbe.enabled`                              | Enable startupProbe on PostgreSQL Primary containers                                                                                                                                                                              | `false`               |
| `primary.startupProbe.initialDelaySeconds`                  | Initial delay seconds for startupProbe                                                                                                                                                                                            | `30`                  |
| `primary.startupProbe.periodSeconds`                        | Period seconds for startupProbe                                                                                                                                                                                                   | `10`                  |
| `primary.startupProbe.timeoutSeconds`                       | Timeout seconds for startupProbe                                                                                                                                                                                                  | `1`                   |
| `primary.startupProbe.failureThreshold`                     | Failure threshold for startupProbe                                                                                                                                                                                                | `15`                  |
| `primary.startupProbe.successThreshold`                     | Success threshold for startupProbe                                                                                                                                                                                                | `1`                   |
| `primary.customLivenessProbe`                               | Custom livenessProbe that overrides the default one                                                                                                                                                                               | `{}`                  |
| `primary.customReadinessProbe`                              | Custom readinessProbe that overrides the default one                                                                                                                                                                              | `{}`                  |
| `primary.customStartupProbe`                                | Custom startupProbe that overrides the default one                                                                                                                                                                                | `{}`                  |
| `primary.lifecycleHooks`                                    | for the PostgreSQL Primary container to automate configuration before or after startup                                                                                                                                            | `{}`                  |
| `primary.resourcesPreset`                                   | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if primary.resources is set (primary.resources is recommended for production). | `nano`                |
| `primary.resources`                                         | Set container requests and limits for different resources like CPU or memory (essential for production workloads)                                                                                                                 | `{}`                  |
| `primary.podSecurityContext.enabled`                        | Enable security context                                                                                                                                                                                                           | `true`                |
| `primary.podSecurityContext.fsGroupChangePolicy`            | Set filesystem group change policy                                                                                                                                                                                                | `Always`              |
| `primary.podSecurityContext.sysctls`                        | Set kernel settings using the sysctl interface                                                                                                                                                                                    | `[]`                  |
| `primary.podSecurityContext.supplementalGroups`             | Set filesystem extra groups                                                                                                                                                                                                       | `[]`                  |
| `primary.podSecurityContext.fsGroup`                        | Group ID for the pod                                                                                                                                                                                                              | `1001`                |
| `primary.containerSecurityContext.enabled`                  | Enabled containers' Security Context                                                                                                                                                                                              | `true`                |
| `primary.containerSecurityContext.seLinuxOptions`           | Set SELinux options in container                                                                                                                                                                                                  | `{}`                  |
| `primary.containerSecurityContext.runAsUser`                | Set containers' Security Context runAsUser                                                                                                                                                                                        | `1001`                |
| `primary.containerSecurityContext.runAsGroup`               | Set containers' Security Context runAsGroup                                                                                                                                                                                       | `1001`                |
| `primary.containerSecurityContext.runAsNonRoot`             | Set container's Security Context runAsNonRoot                                                                                                                                                                                     | `true`                |
| `primary.containerSecurityContext.privileged`               | Set container's Security Context privileged                                                                                                                                                                                       | `false`               |
| `primary.containerSecurityContext.readOnlyRootFilesystem`   | Set container's Security Context readOnlyRootFilesystem                                                                                                                                                                           | `true`                |
| `primary.containerSecurityContext.allowPrivilegeEscalation` | Set container's Security Context allowPrivilegeEscalation                                                                                                                                                                         | `false`               |
| `primary.containerSecurityContext.capabilities.drop`        | List of capabilities to be dropped                                                                                                                                                                                                | `["ALL"]`             |
| `primary.containerSecurityContext.seccompProfile.type`      | Set container's Security Context seccomp profile                                                                                                                                                                                  | `RuntimeDefault`      |
| `primary.automountServiceAccountToken`                      | Mount Service Account token in pod                                                                                                                                                                                                | `false`               |
| `primary.hostAliases`                                       | PostgreSQL primary pods host aliases                                                                                                                                                                                              | `[]`                  |
| `primary.hostNetwork`                                       | Specify if host network should be enabled for PostgreSQL pod (postgresql primary)                                                                                                                                                 | `false`               |
| `primary.hostIPC`                                           | Specify if host IPC should be enabled for PostgreSQL pod (postgresql primary)                                                                                                                                                     | `false`               |
| `primary.labels`                                            | Map of labels to add to the statefulset (postgresql primary)                                                                                                                                                                      | `{}`                  |
| `primary.annotations`                                       | Annotations for PostgreSQL primary pods                                                                                                                                                                                           | `{}`                  |
| `primary.podLabels`                                         | Map of labels to add to the pods (postgresql primary)                                                                                                                                                                             | `{}`                  |
| `primary.podAnnotations`                                    | Map of annotations to add to the pods (postgresql primary)                                                                                                                                                                        | `{}`                  |
| `primary.podAffinityPreset`                                 | PostgreSQL primary pod affinity preset. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                                    | `""`                  |
| `primary.podAntiAffinityPreset`                             | PostgreSQL primary pod anti-affinity preset. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                               | `soft`                |
| `primary.nodeAffinityPreset.type`                           | PostgreSQL primary node affinity preset type. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                              | `""`                  |
| `primary.nodeAffinityPreset.key`                            | PostgreSQL primary node label key to match Ignored if `primary.affinity` is set.                                                                                                                                                  | `""`                  |
| `primary.nodeAffinityPreset.values`                         | PostgreSQL primary node label values to match. Ignored if `primary.affinity` is set.                                                                                                                                              | `[]`                  |
| `primary.affinity`                                          | Affinity for PostgreSQL primary pods assignment                                                                                                                                                                                   | `{}`                  |
| `primary.nodeSelector`                                      | Node labels for PostgreSQL primary pods assignment                                                                                                                                                                                | `{}`                  |
| `primary.tolerations`                                       | Tolerations for PostgreSQL primary pods assignment                                                                                                                                                                                | `[]`                  |
| `primary.topologySpreadConstraints`                         | Topology Spread Constraints for pod assignment spread across your cluster among failure-domains. Evaluated as a template                                                                                                          | `[]`                  |
| `primary.priorityClassName`                                 | Priority Class to use for each pod (postgresql primary)                                                                                                                                                                           | `""`                  |
| `primary.schedulerName`                                     | Use an alternate scheduler, e.g. "stork".                                                                                                                                                                                         | `""`                  |
| `primary.terminationGracePeriodSeconds`                     | Seconds PostgreSQL primary pod needs to terminate gracefully                                                                                                                                                                      | `""`                  |
| `primary.updateStrategy.type`                               | PostgreSQL Primary statefulset strategy type                                                                                                                                                                                      | `RollingUpdate`       |
| `primary.updateStrategy.rollingUpdate`                      | PostgreSQL Primary statefulset rolling update configuration parameters                                                                                                                                                            | `{}`                  |
| `primary.extraVolumeMounts`                                 | Optionally specify extra list of additional volumeMounts for the PostgreSQL Primary container(s)                                                                                                                                  | `[]`                  |
| `primary.extraVolumes`                                      | Optionally specify extra list of additional volumes for the PostgreSQL Primary pod(s)                                                                                                                                             | `[]`                  |
| `primary.sidecars`                                          | Add additional sidecar containers to the PostgreSQL Primary pod(s)                                                                                                                                                                | `[]`                  |
| `primary.initContainers`                                    | Add additional init containers to the PostgreSQL Primary pod(s)                                                                                                                                                                   | `[]`                  |
| `primary.pdb.create`                                        | Enable/disable a Pod Disruption Budget creation                                                                                                                                                                                   | `true`                |
| `primary.pdb.minAvailable`                                  | Minimum number/percentage of pods that should remain scheduled                                                                                                                                                                    | `""`                  |
| `primary.pdb.maxUnavailable`                                | Maximum number/percentage of pods that may be made unavailable. Defaults to `1` if both `primary.pdb.minAvailable` and `primary.pdb.maxUnavailable` are empty.                                                                    | `""`                  |
| `primary.extraPodSpec`                                      | Optionally specify extra PodSpec for the PostgreSQL Primary pod(s)                                                                                                                                                                | `{}`                  |
| `primary.networkPolicy.enabled`                             | Specifies whether a NetworkPolicy should be created                                                                                                                                                                               | `true`                |
| `primary.networkPolicy.allowExternal`                       | Don't require server label for connections                                                                                                                                                                                        | `true`                |
| `primary.networkPolicy.allowExternalEgress`                 | Allow the pod to access any range of port and all destinations.                                                                                                                                                                   | `true`                |
| `primary.networkPolicy.extraIngress`                        | Add extra ingress rules to the NetworkPolicy                                                                                                                                                                                      | `[]`                  |
| `primary.networkPolicy.extraEgress`                         | Add extra ingress rules to the NetworkPolicy                                                                                                                                                                                      | `[]`                  |
| `primary.networkPolicy.ingressNSMatchLabels`                | Labels to match to allow traffic from other namespaces                                                                                                                                                                            | `{}`                  |
| `primary.networkPolicy.ingressNSPodMatchLabels`             | Pod labels to match to allow traffic from other namespaces                                                                                                                                                                        | `{}`                  |
| `primary.service.type`                                      | Kubernetes Service type                                                                                                                                                                                                           | `ClusterIP`           |
| `primary.service.ports.postgresql`                          | PostgreSQL service port                                                                                                                                                                                                           | `5432`                |
| `primary.service.nodePorts.postgresql`                      | Node port for PostgreSQL                                                                                                                                                                                                          | `""`                  |
| `primary.service.clusterIP`                                 | Static clusterIP or None for headless services                                                                                                                                                                                    | `""`                  |
| `primary.service.labels`                                    | Map of labels to add to the primary service                                                                                                                                                                                       | `{}`                  |
| `primary.service.annotations`                               | Annotations for PostgreSQL primary service                                                                                                                                                                                        | `{}`                  |
| `primary.service.loadBalancerClass`                         | Load balancer class if service type is `LoadBalancer`                                                                                                                                                                             | `""`                  |
| `primary.service.loadBalancerIP`                            | Load balancer IP if service type is `LoadBalancer`                                                                                                                                                                                | `""`                  |
| `primary.service.externalTrafficPolicy`                     | Enable client source IP preservation                                                                                                                                                                                              | `Cluster`             |
| `primary.service.loadBalancerSourceRanges`                  | Addresses that are allowed when service is LoadBalancer                                                                                                                                                                           | `[]`                  |
| `primary.service.extraPorts`                                | Extra ports to expose in the PostgreSQL primary service                                                                                                                                                                           | `[]`                  |
| `primary.service.sessionAffinity`                           | Session Affinity for Kubernetes service, can be "None" or "ClientIP"                                                                                                                                                              | `None`                |
| `primary.service.sessionAffinityConfig`                     | Additional settings for the sessionAffinity                                                                                                                                                                                       | `{}`                  |
| `primary.service.headless.annotations`                      | Additional custom annotations for headless PostgreSQL primary service                                                                                                                                                             | `{}`                  |
| `primary.persistence.enabled`                               | Enable PostgreSQL Primary data persistence using PVC                                                                                                                                                                              | `true`                |
| `primary.persistence.volumeName`                            | Name to assign the volume                                                                                                                                                                                                         | `data`                |
| `primary.persistence.existingClaim`                         | Name of an existing PVC to use                                                                                                                                                                                                    | `""`                  |
| `primary.persistence.mountPath`                             | The path the volume will be mounted at                                                                                                                                                                                            | `/bitnami/postgresql` |
| `primary.persistence.subPath`                               | The subdirectory of the volume to mount to                                                                                                                                                                                        | `""`                  |
| `primary.persistence.storageClass`                          | PVC Storage Class for PostgreSQL Primary data volume                                                                                                                                                                              | `""`                  |
| `primary.persistence.accessModes`                           | PVC Access Mode for PostgreSQL volume                                                                                                                                                                                             | `["ReadWriteOnce"]`   |
| `primary.persistence.size`                                  | PVC Storage Request for PostgreSQL volume                                                                                                                                                                                         | `8Gi`                 |
| `primary.persistence.annotations`                           | Annotations for the PVC                                                                                                                                                                                                           | `{}`                  |
| `primary.persistence.labels`                                | Labels for the PVC                                                                                                                                                                                                                | `{}`                  |
| `primary.persistence.selector`                              | Selector to match an existing Persistent Volume (this value is evaluated as a template)                                                                                                                                           | `{}`                  |
| `primary.persistence.dataSource`                            | Custom PVC data source                                                                                                                                                                                                            | `{}`                  |
| `primary.persistentVolumeClaimRetentionPolicy.enabled`      | Enable Persistent volume retention policy for Primary Statefulset                                                                                                                                                                 | `false`               |
| `primary.persistentVolumeClaimRetentionPolicy.whenScaled`   | Volume retention behavior when the replica count of the StatefulSet is reduced                                                                                                                                                    | `Retain`              |
| `primary.persistentVolumeClaimRetentionPolicy.whenDeleted`  | Volume retention behavior that applies when the StatefulSet is deleted                                                                                                                                                            | `Retain`              |

### PostgreSQL read only replica parameters (only used when `architecture` is set to `replication`)

| Name                                                             | Description                                                                                                                                                                                                                                 | Value                 |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| `readReplicas.name`                                              | Name of the read replicas database (eg secondary, slave, ...)                                                                                                                                                                               | `read`                |
| `readReplicas.replicaCount`                                      | Number of PostgreSQL read only replicas                                                                                                                                                                                                     | `1`                   |
| `readReplicas.extendedConfiguration`                             | Extended PostgreSQL read only replicas configuration (appended to main or default configuration)                                                                                                                                            | `""`                  |
| `readReplicas.extraEnvVars`                                      | Array with extra environment variables to add to PostgreSQL read only nodes                                                                                                                                                                 | `[]`                  |
| `readReplicas.extraEnvVarsCM`                                    | Name of existing ConfigMap containing extra env vars for PostgreSQL read only nodes                                                                                                                                                         | `""`                  |
| `readReplicas.extraEnvVarsSecret`                                | Name of existing Secret containing extra env vars for PostgreSQL read only nodes                                                                                                                                                            | `""`                  |
| `readReplicas.command`                                           | Override default container command (useful when using custom images)                                                                                                                                                                        | `[]`                  |
| `readReplicas.args`                                              | Override default container args (useful when using custom images)                                                                                                                                                                           | `[]`                  |
| `readReplicas.livenessProbe.enabled`                             | Enable livenessProbe on PostgreSQL read only containers                                                                                                                                                                                     | `true`                |
| `readReplicas.livenessProbe.initialDelaySeconds`                 | Initial delay seconds for livenessProbe                                                                                                                                                                                                     | `30`                  |
| `readReplicas.livenessProbe.periodSeconds`                       | Period seconds for livenessProbe                                                                                                                                                                                                            | `10`                  |
| `readReplicas.livenessProbe.timeoutSeconds`                      | Timeout seconds for livenessProbe                                                                                                                                                                                                           | `5`                   |
| `readReplicas.livenessProbe.failureThreshold`                    | Failure threshold for livenessProbe                                                                                                                                                                                                         | `6`                   |
| `readReplicas.livenessProbe.successThreshold`                    | Success threshold for livenessProbe                                                                                                                                                                                                         | `1`                   |
| `readReplicas.readinessProbe.enabled`                            | Enable readinessProbe on PostgreSQL read only containers                                                                                                                                                                                    | `true`                |
| `readReplicas.readinessProbe.initialDelaySeconds`                | Initial delay seconds for readinessProbe                                                                                                                                                                                                    | `5`                   |
| `readReplicas.readinessProbe.periodSeconds`                      | Period seconds for readinessProbe                                                                                                                                                                                                           | `10`                  |
| `readReplicas.readinessProbe.timeoutSeconds`                     | Timeout seconds for readinessProbe                                                                                                                                                                                                          | `5`                   |
| `readReplicas.readinessProbe.failureThreshold`                   | Failure threshold for readinessProbe                                                                                                                                                                                                        | `6`                   |
| `readReplicas.readinessProbe.successThreshold`                   | Success threshold for readinessProbe                                                                                                                                                                                                        | `1`                   |
| `readReplicas.startupProbe.enabled`                              | Enable startupProbe on PostgreSQL read only containers                                                                                                                                                                                      | `false`               |
| `readReplicas.startupProbe.initialDelaySeconds`                  | Initial delay seconds for startupProbe                                                                                                                                                                                                      | `30`                  |
| `readReplicas.startupProbe.periodSeconds`                        | Period seconds for startupProbe                                                                                                                                                                                                             | `10`                  |
| `readReplicas.startupProbe.timeoutSeconds`                       | Timeout seconds for startupProbe                                                                                                                                                                                                            | `1`                   |
| `readReplicas.startupProbe.failureThreshold`                     | Failure threshold for startupProbe                                                                                                                                                                                                          | `15`                  |
| `readReplicas.startupProbe.successThreshold`                     | Success threshold for startupProbe                                                                                                                                                                                                          | `1`                   |
| `readReplicas.customLivenessProbe`                               | Custom livenessProbe that overrides the default one                                                                                                                                                                                         | `{}`                  |
| `readReplicas.customReadinessProbe`                              | Custom readinessProbe that overrides the default one                                                                                                                                                                                        | `{}`                  |
| `readReplicas.customStartupProbe`                                | Custom startupProbe that overrides the default one                                                                                                                                                                                          | `{}`                  |
| `readReplicas.lifecycleHooks`                                    | for the PostgreSQL read only container to automate configuration before or after startup                                                                                                                                                    | `{}`                  |
| `readReplicas.resourcesPreset`                                   | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if readReplicas.resources is set (readReplicas.resources is recommended for production). | `nano`                |
| `readReplicas.resources`                                         | Set container requests and limits for different resources like CPU or memory (essential for production workloads)                                                                                                                           | `{}`                  |
| `readReplicas.podSecurityContext.enabled`                        | Enable security context                                                                                                                                                                                                                     | `true`                |
| `readReplicas.podSecurityContext.fsGroupChangePolicy`            | Set filesystem group change policy                                                                                                                                                                                                          | `Always`              |
| `readReplicas.podSecurityContext.sysctls`                        | Set kernel settings using the sysctl interface                                                                                                                                                                                              | `[]`                  |
| `readReplicas.podSecurityContext.supplementalGroups`             | Set filesystem extra groups                                                                                                                                                                                                                 | `[]`                  |
| `readReplicas.podSecurityContext.fsGroup`                        | Group ID for the pod                                                                                                                                                                                                                        | `1001`                |
| `readReplicas.containerSecurityContext.enabled`                  | Enabled containers' Security Context                                                                                                                                                                                                        | `true`                |
| `readReplicas.containerSecurityContext.seLinuxOptions`           | Set SELinux options in container                                                                                                                                                                                                            | `{}`                  |
| `readReplicas.containerSecurityContext.runAsUser`                | Set containers' Security Context runAsUser                                                                                                                                                                                                  | `1001`                |
| `readReplicas.containerSecurityContext.runAsGroup`               | Set containers' Security Context runAsGroup                                                                                                                                                                                                 | `1001`                |
| `readReplicas.containerSecurityContext.runAsNonRoot`             | Set container's Security Context runAsNonRoot                                                                                                                                                                                               | `true`                |
| `readReplicas.containerSecurityContext.privileged`               | Set container's Security Context privileged                                                                                                                                                                                                 | `false`               |
| `readReplicas.containerSecurityContext.readOnlyRootFilesystem`   | Set container's Security Context readOnlyRootFilesystem                                                                                                                                                                                     | `true`                |
| `readReplicas.containerSecurityContext.allowPrivilegeEscalation` | Set container's Security Context allowPrivilegeEscalation                                                                                                                                                                                   | `false`               |
| `readReplicas.containerSecurityContext.capabilities.drop`        | List of capabilities to be dropped                                                                                                                                                                                                          | `["ALL"]`             |
| `readReplicas.containerSecurityContext.seccompProfile.type`      | Set container's Security Context seccomp profile                                                                                                                                                                                            | `RuntimeDefault`      |
| `readReplicas.automountServiceAccountToken`                      | Mount Service Account token in pod                                                                                                                                                                                                          | `false`               |
| `readReplicas.hostAliases`                                       | PostgreSQL read only pods host aliases                                                                                                                                                                                                      | `[]`                  |
| `readReplicas.hostNetwork`                                       | Specify if host network should be enabled for PostgreSQL pod (PostgreSQL read only)                                                                                                                                                         | `false`               |
| `readReplicas.hostIPC`                                           | Specify if host IPC should be enabled for PostgreSQL pod (postgresql primary)                                                                                                                                                               | `false`               |
| `readReplicas.labels`                                            | Map of labels to add to the statefulset (PostgreSQL read only)                                                                                                                                                                              | `{}`                  |
| `readReplicas.annotations`                                       | Annotations for PostgreSQL read only pods                                                                                                                                                                                                   | `{}`                  |
| `readReplicas.podLabels`                                         | Map of labels to add to the pods (PostgreSQL read only)                                                                                                                                                                                     | `{}`                  |
| `readReplicas.podAnnotations`                                    | Map of annotations to add to the pods (PostgreSQL read only)                                                                                                                                                                                | `{}`                  |
| `readReplicas.podAffinityPreset`                                 | PostgreSQL read only pod affinity preset. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                                            | `""`                  |
| `readReplicas.podAntiAffinityPreset`                             | PostgreSQL read only pod anti-affinity preset. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                                       | `soft`                |
| `readReplicas.nodeAffinityPreset.type`                           | PostgreSQL read only node affinity preset type. Ignored if `primary.affinity` is set. Allowed values: `soft` or `hard`                                                                                                                      | `""`                  |
| `readReplicas.nodeAffinityPreset.key`                            | PostgreSQL read only node label key to match Ignored if `primary.affinity` is set.                                                                                                                                                          | `""`                  |
| `readReplicas.nodeAffinityPreset.values`                         | PostgreSQL read only node label values to match. Ignored if `primary.affinity` is set.                                                                                                                                                      | `[]`                  |
| `readReplicas.affinity`                                          | Affinity for PostgreSQL read only pods assignment                                                                                                                                                                                           | `{}`                  |
| `readReplicas.nodeSelector`                                      | Node labels for PostgreSQL read only pods assignment                                                                                                                                                                                        | `{}`                  |
| `readReplicas.tolerations`                                       | Tolerations for PostgreSQL read only pods assignment                                                                                                                                                                                        | `[]`                  |
| `readReplicas.topologySpreadConstraints`                         | Topology Spread Constraints for pod assignment spread across your cluster among failure-domains. Evaluated as a template                                                                                                                    | `[]`                  |
| `readReplicas.priorityClassName`                                 | Priority Class to use for each pod (PostgreSQL read only)                                                                                                                                                                                   | `""`                  |
| `readReplicas.schedulerName`                                     | Use an alternate scheduler, e.g. "stork".                                                                                                                                                                                                   | `""`                  |
| `readReplicas.terminationGracePeriodSeconds`                     | Seconds PostgreSQL read only pod needs to terminate gracefully                                                                                                                                                                              | `""`                  |
| `readReplicas.updateStrategy.type`                               | PostgreSQL read only statefulset strategy type                                                                                                                                                                                              | `RollingUpdate`       |
| `readReplicas.updateStrategy.rollingUpdate`                      | PostgreSQL read only statefulset rolling update configuration parameters                                                                                                                                                                    | `{}`                  |
| `readReplicas.extraVolumeMounts`                                 | Optionally specify extra list of additional volumeMounts for the PostgreSQL read only container(s)                                                                                                                                          | `[]`                  |
| `readReplicas.extraVolumes`                                      | Optionally specify extra list of additional volumes for the PostgreSQL read only pod(s)                                                                                                                                                     | `[]`                  |
| `readReplicas.sidecars`                                          | Add additional sidecar containers to the PostgreSQL read only pod(s)                                                                                                                                                                        | `[]`                  |
| `readReplicas.initContainers`                                    | Add additional init containers to the PostgreSQL read only pod(s)                                                                                                                                                                           | `[]`                  |
| `readReplicas.pdb.create`                                        | Enable/disable a Pod Disruption Budget creation                                                                                                                                                                                             | `true`                |
| `readReplicas.pdb.minAvailable`                                  | Minimum number/percentage of pods that should remain scheduled                                                                                                                                                                              | `""`                  |
| `readReplicas.pdb.maxUnavailable`                                | Maximum number/percentage of pods that may be made unavailable. Defaults to `1` if both `readReplicas.pdb.minAvailable` and `readReplicas.pdb.maxUnavailable` are empty.                                                                    | `""`                  |
| `readReplicas.extraPodSpec`                                      | Optionally specify extra PodSpec for the PostgreSQL read only pod(s)                                                                                                                                                                        | `{}`                  |
| `readReplicas.networkPolicy.enabled`                             | Specifies whether a NetworkPolicy should be created                                                                                                                                                                                         | `true`                |
| `readReplicas.networkPolicy.allowExternal`                       | Don't require server label for connections                                                                                                                                                                                                  | `true`                |
| `readReplicas.networkPolicy.allowExternalEgress`                 | Allow the pod to access any range of port and all destinations.                                                                                                                                                                             | `true`                |
| `readReplicas.networkPolicy.extraIngress`                        | Add extra ingress rules to the NetworkPolicy                                                                                                                                                                                                | `[]`                  |
| `readReplicas.networkPolicy.extraEgress`                         | Add extra ingress rules to the NetworkPolicy                                                                                                                                                                                                | `[]`                  |
| `readReplicas.networkPolicy.ingressNSMatchLabels`                | Labels to match to allow traffic from other namespaces                                                                                                                                                                                      | `{}`                  |
| `readReplicas.networkPolicy.ingressNSPodMatchLabels`             | Pod labels to match to allow traffic from other namespaces                                                                                                                                                                                  | `{}`                  |
| `readReplicas.service.type`                                      | Kubernetes Service type                                                                                                                                                                                                                     | `ClusterIP`           |
| `readReplicas.service.ports.postgresql`                          | PostgreSQL service port                                                                                                                                                                                                                     | `5432`                |
| `readReplicas.service.nodePorts.postgresql`                      | Node port for PostgreSQL                                                                                                                                                                                                                    | `""`                  |
| `readReplicas.service.clusterIP`                                 | Static clusterIP or None for headless services                                                                                                                                                                                              | `""`                  |
| `readReplicas.service.labels`                                    | Map of labels to add to the read service                                                                                                                                                                                                    | `{}`                  |
| `readReplicas.service.annotations`                               | Annotations for PostgreSQL read only service                                                                                                                                                                                                | `{}`                  |
| `readReplicas.service.loadBalancerClass`                         | Load balancer class if service type is `LoadBalancer`                                                                                                                                                                                       | `""`                  |
| `readReplicas.service.loadBalancerIP`                            | Load balancer IP if service type is `LoadBalancer`                                                                                                                                                                                          | `""`                  |
| `readReplicas.service.externalTrafficPolicy`                     | Enable client source IP preservation                                                                                                                                                                                                        | `Cluster`             |
| `readReplicas.service.loadBalancerSourceRanges`                  | Addresses that are allowed when service is LoadBalancer                                                                                                                                                                                     | `[]`                  |
| `readReplicas.service.extraPorts`                                | Extra ports to expose in the PostgreSQL read only service                                                                                                                                                                                   | `[]`                  |
| `readReplicas.service.sessionAffinity`                           | Session Affinity for Kubernetes service, can be "None" or "ClientIP"                                                                                                                                                                        | `None`                |
| `readReplicas.service.sessionAffinityConfig`                     | Additional settings for the sessionAffinity                                                                                                                                                                                                 | `{}`                  |
| `readReplicas.service.headless.annotations`                      | Additional custom annotations for headless PostgreSQL read only service                                                                                                                                                                     | `{}`                  |
| `readReplicas.persistence.enabled`                               | Enable PostgreSQL read only data persistence using PVC                                                                                                                                                                                      | `true`                |
| `readReplicas.persistence.existingClaim`                         | Name of an existing PVC to use                                                                                                                                                                                                              | `""`                  |
| `readReplicas.persistence.mountPath`                             | The path the volume will be mounted at                                                                                                                                                                                                      | `/bitnami/postgresql` |
| `readReplicas.persistence.subPath`                               | The subdirectory of the volume to mount to                                                                                                                                                                                                  | `""`                  |
| `readReplicas.persistence.storageClass`                          | PVC Storage Class for PostgreSQL read only data volume                                                                                                                                                                                      | `""`                  |
| `readReplicas.persistence.accessModes`                           | PVC Access Mode for PostgreSQL volume                                                                                                                                                                                                       | `["ReadWriteOnce"]`   |
| `readReplicas.persistence.size`                                  | PVC Storage Request for PostgreSQL volume                                                                                                                                                                                                   | `8Gi`                 |
| `readReplicas.persistence.annotations`                           | Annotations for the PVC                                                                                                                                                                                                                     | `{}`                  |
| `readReplicas.persistence.labels`                                | Labels for the PVC                                                                                                                                                                                                                          | `{}`                  |
| `readReplicas.persistence.selector`                              | Selector to match an existing Persistent Volume (this value is evaluated as a template)                                                                                                                                                     | `{}`                  |
| `readReplicas.persistence.dataSource`                            | Custom PVC data source                                                                                                                                                                                                                      | `{}`                  |
| `readReplicas.persistentVolumeClaimRetentionPolicy.enabled`      | Enable Persistent volume retention policy for read only Statefulset                                                                                                                                                                         | `false`               |
| `readReplicas.persistentVolumeClaimRetentionPolicy.whenScaled`   | Volume retention behavior when the replica count of the StatefulSet is reduced                                                                                                                                                              | `Retain`              |
| `readReplicas.persistentVolumeClaimRetentionPolicy.whenDeleted`  | Volume retention behavior that applies when the StatefulSet is deleted                                                                                                                                                                      | `Retain`              |

### Backup parameters

| Name                                                               | Description                                                                                                                                                                                                                                     | Value                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `backup.enabled`                                                   | Enable the logical dump of the database "regularly"                                                                                                                                                                                             | `false`                                                                                                                                                                                                                                            |
| `backup.cronjob.schedule`                                          | Set the cronjob parameter schedule                                                                                                                                                                                                              | `@daily`                                                                                                                                                                                                                                           |
| `backup.cronjob.timeZone`                                          | Set the cronjob parameter timeZone                                                                                                                                                                                                              | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.concurrencyPolicy`                                 | Set the cronjob parameter concurrencyPolicy                                                                                                                                                                                                     | `Allow`                                                                                                                                                                                                                                            |
| `backup.cronjob.failedJobsHistoryLimit`                            | Set the cronjob parameter failedJobsHistoryLimit                                                                                                                                                                                                | `1`                                                                                                                                                                                                                                                |
| `backup.cronjob.successfulJobsHistoryLimit`                        | Set the cronjob parameter successfulJobsHistoryLimit                                                                                                                                                                                            | `3`                                                                                                                                                                                                                                                |
| `backup.cronjob.startingDeadlineSeconds`                           | Set the cronjob parameter startingDeadlineSeconds                                                                                                                                                                                               | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.ttlSecondsAfterFinished`                           | Set the cronjob parameter ttlSecondsAfterFinished                                                                                                                                                                                               | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.restartPolicy`                                     | Set the cronjob parameter restartPolicy                                                                                                                                                                                                         | `OnFailure`                                                                                                                                                                                                                                        |
| `backup.cronjob.podSecurityContext.enabled`                        | Enable PodSecurityContext for CronJob/Backup                                                                                                                                                                                                    | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.podSecurityContext.fsGroupChangePolicy`            | Set filesystem group change policy                                                                                                                                                                                                              | `Always`                                                                                                                                                                                                                                           |
| `backup.cronjob.podSecurityContext.sysctls`                        | Set kernel settings using the sysctl interface                                                                                                                                                                                                  | `[]`                                                                                                                                                                                                                                               |
| `backup.cronjob.podSecurityContext.supplementalGroups`             | Set filesystem extra groups                                                                                                                                                                                                                     | `[]`                                                                                                                                                                                                                                               |
| `backup.cronjob.podSecurityContext.fsGroup`                        | Group ID for the CronJob                                                                                                                                                                                                                        | `1001`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.enabled`                  | Enabled containers' Security Context                                                                                                                                                                                                            | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.seLinuxOptions`           | Set SELinux options in container                                                                                                                                                                                                                | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.containerSecurityContext.runAsUser`                | Set containers' Security Context runAsUser                                                                                                                                                                                                      | `1001`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.runAsGroup`               | Set containers' Security Context runAsGroup                                                                                                                                                                                                     | `1001`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.runAsNonRoot`             | Set container's Security Context runAsNonRoot                                                                                                                                                                                                   | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.privileged`               | Set container's Security Context privileged                                                                                                                                                                                                     | `false`                                                                                                                                                                                                                                            |
| `backup.cronjob.containerSecurityContext.readOnlyRootFilesystem`   | Set container's Security Context readOnlyRootFilesystem                                                                                                                                                                                         | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.containerSecurityContext.allowPrivilegeEscalation` | Set container's Security Context allowPrivilegeEscalation                                                                                                                                                                                       | `false`                                                                                                                                                                                                                                            |
| `backup.cronjob.containerSecurityContext.capabilities.drop`        | List of capabilities to be dropped                                                                                                                                                                                                              | `["ALL"]`                                                                                                                                                                                                                                          |
| `backup.cronjob.containerSecurityContext.seccompProfile.type`      | Set container's Security Context seccomp profile                                                                                                                                                                                                | `RuntimeDefault`                                                                                                                                                                                                                                   |
| `backup.cronjob.command`                                           | Set backup container's command to run                                                                                                                                                                                                           | `["/bin/bash","-c","PGPASSWORD=\"${PGPASSWORD:-$(< \"$PGPASSWORD_FILE\")}\" pg_dumpall --clean --if-exists --load-via-partition-root --quote-all-identifiers --no-password --file=\"${PGDUMP_DIR}/pg_dumpall-$(date '+%Y-%m-%d-%H-%M').pgdump\""]` |
| `backup.cronjob.labels`                                            | Set the cronjob labels                                                                                                                                                                                                                          | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.annotations`                                       | Set the cronjob annotations                                                                                                                                                                                                                     | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.nodeSelector`                                      | Node labels for PostgreSQL backup CronJob pod assignment                                                                                                                                                                                        | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.tolerations`                                       | Tolerations for PostgreSQL backup CronJob pods assignment                                                                                                                                                                                       | `[]`                                                                                                                                                                                                                                               |
| `backup.cronjob.resourcesPreset`                                   | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if backup.cronjob.resources is set (backup.cronjob.resources is recommended for production). | `nano`                                                                                                                                                                                                                                             |
| `backup.cronjob.resources`                                         | Set container requests and limits for different resources like CPU or memory                                                                                                                                                                    | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.networkPolicy.enabled`                             | Specifies whether a NetworkPolicy should be created                                                                                                                                                                                             | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.storage.enabled`                                   | Enable using a `PersistentVolumeClaim` as backup data volume                                                                                                                                                                                    | `true`                                                                                                                                                                                                                                             |
| `backup.cronjob.storage.existingClaim`                             | Provide an existing `PersistentVolumeClaim` (only when `architecture=standalone`)                                                                                                                                                               | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.storage.resourcePolicy`                            | Setting it to "keep" to avoid removing PVCs during a helm delete operation. Leaving it empty will delete PVCs after the chart deleted                                                                                                           | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.storage.storageClass`                              | PVC Storage Class for the backup data volume                                                                                                                                                                                                    | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.storage.accessModes`                               | PV Access Mode                                                                                                                                                                                                                                  | `["ReadWriteOnce"]`                                                                                                                                                                                                                                |
| `backup.cronjob.storage.size`                                      | PVC Storage Request for the backup data volume                                                                                                                                                                                                  | `8Gi`                                                                                                                                                                                                                                              |
| `backup.cronjob.storage.annotations`                               | PVC annotations                                                                                                                                                                                                                                 | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.storage.mountPath`                                 | Path to mount the volume at                                                                                                                                                                                                                     | `/backup/pgdump`                                                                                                                                                                                                                                   |
| `backup.cronjob.storage.subPath`                                   | Subdirectory of the volume to mount at                                                                                                                                                                                                          | `""`                                                                                                                                                                                                                                               |
| `backup.cronjob.storage.volumeClaimTemplates.selector`             | A label query over volumes to consider for binding (e.g. when using local volumes)                                                                                                                                                              | `{}`                                                                                                                                                                                                                                               |
| `backup.cronjob.extraVolumeMounts`                                 | Optionally specify extra list of additional volumeMounts for the backup container                                                                                                                                                               | `[]`                                                                                                                                                                                                                                               |
| `backup.cronjob.extraVolumes`                                      | Optionally specify extra list of additional volumes for the backup container                                                                                                                                                                    | `[]`                                                                                                                                                                                                                                               |

### Password update job

| Name                                                                  | Description                                                                                                                                                                                                                                           | Value            |
| --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| `passwordUpdateJob.enabled`                                           | Enable password update job                                                                                                                                                                                                                            | `false`          |
| `passwordUpdateJob.backoffLimit`                                      | set backoff limit of the job                                                                                                                                                                                                                          | `10`             |
| `passwordUpdateJob.command`                                           | Override default container command on mysql Primary container(s) (useful when using custom images)                                                                                                                                                    | `[]`             |
| `passwordUpdateJob.args`                                              | Override default container args on mysql Primary container(s) (useful when using custom images)                                                                                                                                                       | `[]`             |
| `passwordUpdateJob.extraCommands`                                     | Extra commands to pass to the generation job                                                                                                                                                                                                          | `""`             |
| `passwordUpdateJob.previousPasswords.postgresPassword`                | Previous postgres password (set if the password secret was already changed)                                                                                                                                                                           | `""`             |
| `passwordUpdateJob.previousPasswords.password`                        | Previous password (set if the password secret was already changed)                                                                                                                                                                                    | `""`             |
| `passwordUpdateJob.previousPasswords.replicationPassword`             | Previous replication password (set if the password secret was already changed)                                                                                                                                                                        | `""`             |
| `passwordUpdateJob.previousPasswords.existingSecret`                  | Name of a secret containing the previous passwords (set if the password secret was already changed)                                                                                                                                                   | `""`             |
| `passwordUpdateJob.containerSecurityContext.enabled`                  | Enabled containers' Security Context                                                                                                                                                                                                                  | `true`           |
| `passwordUpdateJob.containerSecurityContext.seLinuxOptions`           | Set SELinux options in container                                                                                                                                                                                                                      | `{}`             |
| `passwordUpdateJob.containerSecurityContext.runAsUser`                | Set containers' Security Context runAsUser                                                                                                                                                                                                            | `1001`           |
| `passwordUpdateJob.containerSecurityContext.runAsGroup`               | Set containers' Security Context runAsGroup                                                                                                                                                                                                           | `1001`           |
| `passwordUpdateJob.containerSecurityContext.runAsNonRoot`             | Set container's Security Context runAsNonRoot                                                                                                                                                                                                         | `true`           |
| `passwordUpdateJob.containerSecurityContext.privileged`               | Set container's Security Context privileged                                                                                                                                                                                                           | `false`          |
| `passwordUpdateJob.containerSecurityContext.readOnlyRootFilesystem`   | Set container's Security Context readOnlyRootFilesystem                                                                                                                                                                                               | `true`           |
| `passwordUpdateJob.containerSecurityContext.allowPrivilegeEscalation` | Set container's Security Context allowPrivilegeEscalation                                                                                                                                                                                             | `false`          |
| `passwordUpdateJob.containerSecurityContext.capabilities.drop`        | List of capabilities to be dropped                                                                                                                                                                                                                    | `["ALL"]`        |
| `passwordUpdateJob.containerSecurityContext.seccompProfile.type`      | Set container's Security Context seccomp profile                                                                                                                                                                                                      | `RuntimeDefault` |
| `passwordUpdateJob.podSecurityContext.enabled`                        | Enabled credential init job pods' Security Context                                                                                                                                                                                                    | `true`           |
| `passwordUpdateJob.podSecurityContext.fsGroupChangePolicy`            | Set filesystem group change policy                                                                                                                                                                                                                    | `Always`         |
| `passwordUpdateJob.podSecurityContext.sysctls`                        | Set kernel settings using the sysctl interface                                                                                                                                                                                                        | `[]`             |
| `passwordUpdateJob.podSecurityContext.supplementalGroups`             | Set filesystem extra groups                                                                                                                                                                                                                           | `[]`             |
| `passwordUpdateJob.podSecurityContext.fsGroup`                        | Set credential init job pod's Security Context fsGroup                                                                                                                                                                                                | `1001`           |
| `passwordUpdateJob.extraEnvVars`                                      | Array containing extra env vars to configure the credential init job                                                                                                                                                                                  | `[]`             |
| `passwordUpdateJob.extraEnvVarsCM`                                    | ConfigMap containing extra env vars to configure the credential init job                                                                                                                                                                              | `""`             |
| `passwordUpdateJob.extraEnvVarsSecret`                                | Secret containing extra env vars to configure the credential init job (in case of sensitive data)                                                                                                                                                     | `""`             |
| `passwordUpdateJob.extraVolumes`                                      | Optionally specify extra list of additional volumes for the credential init job                                                                                                                                                                       | `[]`             |
| `passwordUpdateJob.extraVolumeMounts`                                 | Array of extra volume mounts to be added to the jwt Container (evaluated as template). Normally used with `extraVolumes`.                                                                                                                             | `[]`             |
| `passwordUpdateJob.initContainers`                                    | Add additional init containers for the mysql Primary pod(s)                                                                                                                                                                                           | `[]`             |
| `passwordUpdateJob.resourcesPreset`                                   | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if passwordUpdateJob.resources is set (passwordUpdateJob.resources is recommended for production). | `micro`          |
| `passwordUpdateJob.resources`                                         | Set container requests and limits for different resources like CPU or memory (essential for production workloads)                                                                                                                                     | `{}`             |
| `passwordUpdateJob.customLivenessProbe`                               | Custom livenessProbe that overrides the default one                                                                                                                                                                                                   | `{}`             |
| `passwordUpdateJob.customReadinessProbe`                              | Custom readinessProbe that overrides the default one                                                                                                                                                                                                  | `{}`             |
| `passwordUpdateJob.customStartupProbe`                                | Custom startupProbe that overrides the default one                                                                                                                                                                                                    | `{}`             |
| `passwordUpdateJob.automountServiceAccountToken`                      | Mount Service Account token in pod                                                                                                                                                                                                                    | `false`          |
| `passwordUpdateJob.hostAliases`                                       | Add deployment host aliases                                                                                                                                                                                                                           | `[]`             |
| `passwordUpdateJob.annotations`                                       | Add annotations to the job                                                                                                                                                                                                                            | `{}`             |
| `passwordUpdateJob.podLabels`                                         | Additional pod labels                                                                                                                                                                                                                                 | `{}`             |
| `passwordUpdateJob.podAnnotations`                                    | Additional pod annotations                                                                                                                                                                                                                            | `{}`             |

### Volume Permissions parameters

| Name                                                             | Description                                                                                                                                                                                                                                           | Value                      |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `volumePermissions.enabled`                                      | Enable init container that changes the owner and group of the persistent volume                                                                                                                                                                       | `false`                    |
| `volumePermissions.image.registry`                               | Init container volume-permissions image registry                                                                                                                                                                                                      | `REGISTRY_NAME`            |
| `volumePermissions.image.repository`                             | Init container volume-permissions image repository                                                                                                                                                                                                    | `REPOSITORY_NAME/os-shell` |
| `volumePermissions.image.digest`                                 | Init container volume-permissions image digest in the way sha256:aa.... Please note this parameter, if set, will override the tag                                                                                                                     | `""`                       |
| `volumePermissions.image.pullPolicy`                             | Init container volume-permissions image pull policy                                                                                                                                                                                                   | `IfNotPresent`             |
| `volumePermissions.image.pullSecrets`                            | Init container volume-permissions image pull secrets                                                                                                                                                                                                  | `[]`                       |
| `volumePermissions.resourcesPreset`                              | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if volumePermissions.resources is set (volumePermissions.resources is recommended for production). | `nano`                     |
| `volumePermissions.resources`                                    | Set container requests and limits for different resources like CPU or memory (essential for production workloads)                                                                                                                                     | `{}`                       |
| `volumePermissions.containerSecurityContext.seLinuxOptions`      | Set SELinux options in container                                                                                                                                                                                                                      | `{}`                       |
| `volumePermissions.containerSecurityContext.runAsUser`           | User ID for the init container                                                                                                                                                                                                                        | `0`                        |
| `volumePermissions.containerSecurityContext.runAsGroup`          | Group ID for the init container                                                                                                                                                                                                                       | `0`                        |
| `volumePermissions.containerSecurityContext.runAsNonRoot`        | runAsNonRoot for the init container                                                                                                                                                                                                                   | `false`                    |
| `volumePermissions.containerSecurityContext.seccompProfile.type` | seccompProfile.type for the init container                                                                                                                                                                                                            | `RuntimeDefault`           |

### Other Parameters

| Name                                          | Description                                                                                                                                 | Value   |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `serviceBindings.enabled`                     | Create secret for service binding (Experimental)                                                                                            | `false` |
| `serviceAccount.create`                       | Enable creation of ServiceAccount for PostgreSQL pod                                                                                        | `true`  |
| `serviceAccount.name`                         | The name of the ServiceAccount to use.                                                                                                      | `""`    |
| `serviceAccount.automountServiceAccountToken` | Allows auto mount of ServiceAccountToken on the serviceAccount created                                                                      | `false` |
| `serviceAccount.annotations`                  | Additional custom annotations for the ServiceAccount                                                                                        | `{}`    |
| `rbac.create`                                 | Create Role and RoleBinding (required for PSP to work)                                                                                      | `false` |
| `rbac.rules`                                  | Custom RBAC rules to set                                                                                                                    | `[]`    |
| `psp.create`                                  | Whether to create a PodSecurityPolicy. WARNING: PodSecurityPolicy is deprecated in Kubernetes v1.21 or later, unavailable in v1.25 or later | `false` |

### Metrics Parameters

| Name                                                        | Description                                                                                                                                                                                                                       | Value                               |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- |
| `metrics.enabled`                                           | Start a prometheus exporter                                                                                                                                                                                                       | `false`                             |
| `metrics.image.registry`                                    | PostgreSQL Prometheus Exporter image registry                                                                                                                                                                                     | `REGISTRY_NAME`                     |
| `metrics.image.repository`                                  | PostgreSQL Prometheus Exporter image repository                                                                                                                                                                                   | `REPOSITORY_NAME/postgres-exporter` |
| `metrics.image.digest`                                      | PostgreSQL image digest in the way sha256:aa.... Please note this parameter, if set, will override the tag                                                                                                                        | `""`                                |
| `metrics.image.pullPolicy`                                  | PostgreSQL Prometheus Exporter image pull policy                                                                                                                                                                                  | `IfNotPresent`                      |
| `metrics.image.pullSecrets`                                 | Specify image pull secrets                                                                                                                                                                                                        | `[]`                                |
| `metrics.collectors`                                        | Control enabled collectors                                                                                                                                                                                                        | `{}`                                |
| `metrics.customMetrics`                                     | Define additional custom metrics                                                                                                                                                                                                  | `{}`                                |
| `metrics.extraEnvVars`                                      | Extra environment variables to add to PostgreSQL Prometheus exporter                                                                                                                                                              | `[]`                                |
| `metrics.containerSecurityContext.enabled`                  | Enabled containers' Security Context                                                                                                                                                                                              | `true`                              |
| `metrics.containerSecurityContext.seLinuxOptions`           | Set SELinux options in container                                                                                                                                                                                                  | `{}`                                |
| `metrics.containerSecurityContext.runAsUser`                | Set containers' Security Context runAsUser                                                                                                                                                                                        | `1001`                              |
| `metrics.containerSecurityContext.runAsGroup`               | Set containers' Security Context runAsGroup                                                                                                                                                                                       | `1001`                              |
| `metrics.containerSecurityContext.runAsNonRoot`             | Set container's Security Context runAsNonRoot                                                                                                                                                                                     | `true`                              |
| `metrics.containerSecurityContext.privileged`               | Set container's Security Context privileged                                                                                                                                                                                       | `false`                             |
| `metrics.containerSecurityContext.readOnlyRootFilesystem`   | Set container's Security Context readOnlyRootFilesystem                                                                                                                                                                           | `true`                              |
| `metrics.containerSecurityContext.allowPrivilegeEscalation` | Set container's Security Context allowPrivilegeEscalation                                                                                                                                                                         | `false`                             |
| `metrics.containerSecurityContext.capabilities.drop`        | List of capabilities to be dropped                                                                                                                                                                                                | `["ALL"]`                           |
| `metrics.containerSecurityContext.seccompProfile.type`      | Set container's Security Context seccomp profile                                                                                                                                                                                  | `RuntimeDefault`                    |
| `metrics.livenessProbe.enabled`                             | Enable livenessProbe on PostgreSQL Prometheus exporter containers                                                                                                                                                                 | `true`                              |
| `metrics.livenessProbe.initialDelaySeconds`                 | Initial delay seconds for livenessProbe                                                                                                                                                                                           | `5`                                 |
| `metrics.livenessProbe.periodSeconds`                       | Period seconds for livenessProbe                                                                                                                                                                                                  | `10`                                |
| `metrics.livenessProbe.timeoutSeconds`                      | Timeout seconds for livenessProbe                                                                                                                                                                                                 | `5`                                 |
| `metrics.livenessProbe.failureThreshold`                    | Failure threshold for livenessProbe                                                                                                                                                                                               | `6`                                 |
| `metrics.livenessProbe.successThreshold`                    | Success threshold for livenessProbe                                                                                                                                                                                               | `1`                                 |
| `metrics.readinessProbe.enabled`                            | Enable readinessProbe on PostgreSQL Prometheus exporter containers                                                                                                                                                                | `true`                              |
| `metrics.readinessProbe.initialDelaySeconds`                | Initial delay seconds for readinessProbe                                                                                                                                                                                          | `5`                                 |
| `metrics.readinessProbe.periodSeconds`                      | Period seconds for readinessProbe                                                                                                                                                                                                 | `10`                                |
| `metrics.readinessProbe.timeoutSeconds`                     | Timeout seconds for readinessProbe                                                                                                                                                                                                | `5`                                 |
| `metrics.readinessProbe.failureThreshold`                   | Failure threshold for readinessProbe                                                                                                                                                                                              | `6`                                 |
| `metrics.readinessProbe.successThreshold`                   | Success threshold for readinessProbe                                                                                                                                                                                              | `1`                                 |
| `metrics.startupProbe.enabled`                              | Enable startupProbe on PostgreSQL Prometheus exporter containers                                                                                                                                                                  | `false`                             |
| `metrics.startupProbe.initialDelaySeconds`                  | Initial delay seconds for startupProbe                                                                                                                                                                                            | `10`                                |
| `metrics.startupProbe.periodSeconds`                        | Period seconds for startupProbe                                                                                                                                                                                                   | `10`                                |
| `metrics.startupProbe.timeoutSeconds`                       | Timeout seconds for startupProbe                                                                                                                                                                                                  | `1`                                 |
| `metrics.startupProbe.failureThreshold`                     | Failure threshold for startupProbe                                                                                                                                                                                                | `15`                                |
| `metrics.startupProbe.successThreshold`                     | Success threshold for startupProbe                                                                                                                                                                                                | `1`                                 |
| `metrics.customLivenessProbe`                               | Custom livenessProbe that overrides the default one                                                                                                                                                                               | `{}`                                |
| `metrics.customReadinessProbe`                              | Custom readinessProbe that overrides the default one                                                                                                                                                                              | `{}`                                |
| `metrics.customStartupProbe`                                | Custom startupProbe that overrides the default one                                                                                                                                                                                | `{}`                                |
| `metrics.containerPorts.metrics`                            | PostgreSQL Prometheus exporter metrics container port                                                                                                                                                                             | `9187`                              |
| `metrics.resourcesPreset`                                   | Set container resources according to one common preset (allowed values: none, nano, micro, small, medium, large, xlarge, 2xlarge). This is ignored if metrics.resources is set (metrics.resources is recommended for production). | `nano`                              |
| `metrics.resources`                                         | Set container requests and limits for different resources like CPU or memory (essential for production workloads)                                                                                                                 | `{}`                                |
| `metrics.service.ports.metrics`                             | PostgreSQL Prometheus Exporter service port                                                                                                                                                                                       | `9187`                              |
| `metrics.service.clusterIP`                                 | Static clusterIP or None for headless services                                                                                                                                                                                    | `""`                                |
| `metrics.service.sessionAffinity`                           | Control where client requests go, to the same pod or round-robin                                                                                                                                                                  | `None`                              |
| `metrics.service.annotations`                               | Annotations for Prometheus to auto-discover the metrics endpoint                                                                                                                                                                  | `{}`                                |
| `metrics.serviceMonitor.enabled`                            | Create ServiceMonitor Resource for scraping metrics using Prometheus Operator                                                                                                                                                     | `false`                             |
| `metrics.serviceMonitor.namespace`                          | Namespace for the ServiceMonitor Resource (defaults to the Release Namespace)                                                                                                                                                     | `""`                                |
| `metrics.serviceMonitor.interval`                           | Interval at which metrics should be scraped.                                                                                                                                                                                      | `""`                                |
| `metrics.serviceMonitor.scrapeTimeout`                      | Timeout after which the scrape is ended                                                                                                                                                                                           | `""`                                |
| `metrics.serviceMonitor.labels`                             | Additional labels that can be used so ServiceMonitor will be discovered by Prometheus                                                                                                                                             | `{}`                                |
| `metrics.serviceMonitor.selector`                           | Prometheus instance selector labels                                                                                                                                                                                               | `{}`                                |
| `metrics.serviceMonitor.relabelings`                        | RelabelConfigs to apply to samples before scraping                                                                                                                                                                                | `[]`                                |
| `metrics.serviceMonitor.metricRelabelings`                  | MetricRelabelConfigs to apply to samples before ingestion                                                                                                                                                                         | `[]`                                |
| `metrics.serviceMonitor.honorLabels`                        | Specify honorLabels parameter to add the scrape endpoint                                                                                                                                                                          | `false`                             |
| `metrics.serviceMonitor.jobLabel`                           | The name of the label on the target service to use as the job name in prometheus.                                                                                                                                                 | `""`                                |
| `metrics.prometheusRule.enabled`                            | Create a PrometheusRule for Prometheus Operator                                                                                                                                                                                   | `false`                             |
| `metrics.prometheusRule.namespace`                          | Namespace for the PrometheusRule Resource (defaults to the Release Namespace)                                                                                                                                                     | `""`                                |
| `metrics.prometheusRule.labels`                             | Additional labels that can be used so PrometheusRule will be discovered by Prometheus                                                                                                                                             | `{}`                                |
| `metrics.prometheusRule.rules`                              | PrometheusRule definitions                                                                                                                                                                                                        | `[]`                                |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. For example,

```console
helm install my-release \
    --set auth.postgresPassword=secretpassword
    oci://REGISTRY_NAME/REPOSITORY_NAME/postgresql
```

> Note: You need to substitute the placeholders `REGISTRY_NAME` and `REPOSITORY_NAME` with a reference to your Helm chart registry and repository. For example, in the case of Bitnami, you need to use `REGISTRY_NAME=registry-1.docker.io` and `REPOSITORY_NAME=bitnamicharts`.

The above command sets the PostgreSQL `postgres` account password to `secretpassword`.

> NOTE: Once this chart is deployed, it is not possible to change the application's access credentials, such as usernames or passwords, using Helm. To change these application credentials after deployment, delete any persistent volumes (PVs) used by the chart and re-deploy it, or use the application's built-in administrative tools if available.
> **Warning** Setting a password will be ignored on new installation in case when previous PostgreSQL release was deleted through the helm command. In that case, old PVC will have an old password, and setting it through helm won't take effect. Deleting persistent volumes (PVs) will solve the issue. Refer to [issue 2061](https://github.com/bitnami/charts/issues/2061) for more details

Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example,

```console
helm install my-release -f values.yaml oci://REGISTRY_NAME/REPOSITORY_NAME/postgresql
```

> Note: You need to substitute the placeholders `REGISTRY_NAME` and `REPOSITORY_NAME` with a reference to your Helm chart registry and repository. For example, in the case of Bitnami, you need to use `REGISTRY_NAME=registry-1.docker.io` and `REPOSITORY_NAME=bitnamicharts`.
> **Tip**: You can use the default [values.yaml](https://github.com/bitnami/charts/tree/main/bitnami/postgresql/values.yaml)

## Troubleshooting

Find more information about how to deal with common errors related to Bitnami's Helm charts in [this troubleshooting guide](https://docs.bitnami.com/general/how-to/troubleshoot-helm-chart-issues).

## Upgrading

### To 16.3.0

This version introduces image verification for security purposes. To disable it, set `global.security.allowInsecureImages` to `true`. More details at [GitHub issue](https://github.com/bitnami/charts/issues/30850).

### To 15.0.0

This major bump changes the following security defaults:

- `runAsGroup` is changed from `0` to `1001`
- `readOnlyRootFilesystem` is set to `true`
- `resourcesPreset` is changed from `none` to the minimum size working in our test suites (NOTE: `resourcesPreset` is not meant for production usage, but `resources` adapted to your use case).
- `global.compatibility.openshift.adaptSecurityContext` is changed from `disabled` to `auto`.

This could potentially break any customization or init scripts used in your deployment. If this is the case, change the default values to the previous ones.

### To 14.0.0

This major version adapts the NetworkPolicy objects to the most recent Bitnami standards. Now there is a separate object for `primary` and for `readReplicas`, being located in their corresponding sections. It is also enabled by default in other to comply with the best security standards.

Check the parameter section for the new value structure.

### To 13.0.0

This major version changes the default PostgreSQL image from 15.x to 16.x. Follow the [official instructions](https://www.postgresql.org/docs/16/upgrading.html) to upgrade to 16.x.

### To 12.0.0

This major version changes the default PostgreSQL image from 14.x to 15.x. Follow the [official instructions](https://www.postgresql.org/docs/15/upgrading.html) to upgrade to 15.x.

### To 11.0.0

In this version the application version was bumped to _14.x_ series. Also, this major release renames several values in this chart and adds missing features, in order to be inline with the rest of assets in the Bitnami charts repository.

- _replication.enabled_ parameter is deprecated in favor of _architecture_ parameter that accepts two values: _standalone_ and _replication_.
- _replication.singleService_ and _replication.uniqueServices_ parameters are deprecated. When using replication, each statefulset (primary and read-only) has its own headless service & service allowing to connect to read-only replicas through the service (round-robin) or individually.
- _postgresqlPostgresPassword_, _postgresqlUsername_, _postgresqlPassword_, _postgresqlDatabase_, _replication.user_, _replication.password_, and _existingSecret_ parameters have been regrouped under the _auth_ map. The _auth_ map uses a new perspective to configure authentication, so please read carefully each sub-parameter description.
- _extraEnv_ has been deprecated in favor of _primary.extraEnvVars_ and _readReplicas.extraEnvVars_.
- _postgresqlConfiguration_, _pgHbaConfiguration_, _configurationConfigMap_, _postgresqlExtendedConf_, and _extendedConfConfigMap_ have been deprecated in favor of _primary.configuration_, _primary.pgHbaConfiguration_, _primary.existingConfigmap_, _primary.extendedConfiguration_, and _primary.existingExtendedConfigmap_.
- _postgresqlInitdbArgs_, _postgresqlInitdbWalDir_, _initdbScripts_, _initdbScriptsConfigMap_, _initdbScriptsSecret_, _initdbUser_ and _initdbPassword_ have been regrouped under the _primary.initdb_ map.
- _postgresqlMaxConnections_, _postgresqlPostgresConnectionLimit_, _postgresqlDbUserConnectionLimit_, _postgresqlTcpKeepalivesInterval_, _postgresqlTcpKeepalivesIdle_, _postgresqlTcpKeepalivesCount_, _postgresqlStatementTimeout_ and _postgresqlPghbaRemoveFilters_ parameters are deprecated. Use _XXX.extraEnvVars_ instead.
- _primaryAsStandBy_ has been deprecated in favor of _primary.standby_.
- _securityContext_ and _containerSecurityContext_ have been deprecated in favor of _primary.podSecurityContext_, _primary.containerSecurityContext_, _readReplicas.podSecurityContext_, and _readReplicas.containerSecurityContext_.
- _livenessProbe_ and _readinessProbe_ maps have been deprecated in favor of _primary.livenessProbe_, _primary.readinessProbe_, _readReplicas.livenessProbe_ and _readReplicas.readinessProbe_ maps.
- _persistence_ map has been deprecated in favor of _primary.persistence_ and _readReplicas.persistence_ maps.
- _networkPolicy_ map has been completely refactored.
- _service_ map has been deprecated in favor of _primary.service_ and _readReplicas.service_ maps.
- _metrics.service.port_ has been regrouped under the _metrics.service.ports_ map.
- _serviceAccount.enabled_ and _serviceAccount.autoMount_ have been deprecated in favor of _serviceAccount.create_ and _serviceAccount.automountServiceAccountToken_.

#### How to upgrade to version 11.0.0

To upgrade to _11.0.0_ from _10.x_, it should be done reusing the PVC(s) used to hold the PostgreSQL data on your previous release. To do so, follow the instructions below (the following example assumes that the release name is _postgresql_):

> NOTE: Please, create a backup of your database before running any of these actions.

1. Obtain the credentials and the names of the PVCs used to hold the PostgreSQL data on your current release:

```console
export POSTGRESQL_PASSWORD=$(kubectl get secret --namespace default postgresql -o jsonpath="{.data.postgresql-password}" | base64 --decode)
export POSTGRESQL_PVC=$(kubectl get pvc -l app.kubernetes.io/instance=postgresql,role=primary -o jsonpath="{.items[0].metadata.name}")
```

1. Delete the PostgreSQL statefulset (notice the option _--cascade=false_) and secret:

```console
kubectl delete statefulsets.apps postgresql-postgresql --namespace default --cascade=false
kubectl delete secret postgresql --namespace default
```

1. Upgrade your release using the same PostgreSQL version:

```console
CURRENT_VERSION=$(kubectl exec postgresql-postgresql-0 -- bash -c 'echo $BITNAMI_IMAGE_VERSION')
helm upgrade postgresql bitnami/postgresql \
  --set auth.postgresPassword=$POSTGRESQL_PASSWORD \
  --set primary.persistence.existingClaim=$POSTGRESQL_PVC \
  --set image.tag=$CURRENT_VERSION
```

1. You will have to delete the existing PostgreSQL pod and the new statefulset is going to create a new one

```console
kubectl delete pod postgresql-postgresql-0
```

1. Finally, you should see the lines below in PostgreSQL container logs:

```text
$ kubectl logs $(kubectl get pods -l app.kubernetes.io/instance=postgresql,app.kubernetes.io/name=postgresql,app.kubernetes.io/component=primary -o jsonpath="{.items[0].metadata.name}")
...
postgresql 08:05:12.59 INFO  ==> Deploying PostgreSQL with persisted data...
...
```

> NOTE: the instructions above reuse the same PostgreSQL version you were using in your chart release. Otherwise, you will find an error such as the one below when upgrading since the new chart major version also bumps the application version. To workaround this issue you need to upgrade database, please refer to the [official PostgreSQL documentation](https://www.postgresql.org/docs/current/upgrading.html) for more information about this.

```console
$ kubectl logs $(kubectl get pods -l app.kubernetes.io/instance=postgresql,app.kubernetes.io/name=postgresql,app.kubernetes.io/component=primary -o jsonpath="{.items[0].metadata.name}")
    ...
postgresql 08:10:14.72 INFO  ==> ** Starting PostgreSQL **
2022-02-01 08:10:14.734 GMT [1] FATAL:  database files are incompatible with server
2022-02-01 08:10:14.734 GMT [1] DETAIL:  The data directory was initialized by PostgreSQL version 11, which is not compatible with this version 14.1.
```

### To 10.0.0

[On November 13, 2020, Helm v2 support was formally finished](https://github.com/helm/charts#status-of-the-project), this major version is the result of the required changes applied to the Helm Chart to be able to incorporate the different features added in Helm v3 and to be consistent with the Helm project itself regarding the Helm v2 EOL.

- Previous versions of this Helm Chart use `apiVersion: v1` (installable by both Helm 2 and 3), this Helm Chart was updated to `apiVersion: v2` (installable by Helm 3 only). [Here](https://helm.sh/docs/topics/charts/#the-apiversion-field) you can find more information about the `apiVersion` field.
- Move dependency information from the _requirements.yaml_ to the _Chart.yaml_
- After running _helm dependency update_, a _Chart.lock_ file is generated containing the same structure used in the previous _requirements.lock_
- The different fields present in the _Chart.yaml_ file has been ordered alphabetically in a homogeneous way for all the Bitnami Helm Chart.
- The term _master_ has been replaced with _primary_ and _slave_ with _readReplicas_ throughout the chart. Role names have changed from _master_ and _slave_ to _primary_ and _read_.

#### Considerations when upgrading to this version

- If you want to upgrade to this version using Helm v2, this scenario is not supported as this version does not support Helm v2 anymore.
- If you installed the previous version with Helm v2 and wants to upgrade to this version with Helm v3, please refer to the [official Helm documentation](https://helm.sh/docs/topics/v2_v3_migration/#migration-use-cases) about migrating from Helm v2 to v3.

#### Useful links

- [Bitnami Tutorial](https://techdocs.broadcom.com/us/en/vmware-tanzu/application-catalog/tanzu-application-catalog/services/tac-doc/apps-tutorials-resolve-helm2-helm3-post-migration-issues-index.html)
- [Helm docs](https://helm.sh/docs/topics/v2_v3_migration)
- [Helm Blog](https://helm.sh/blog/migrate-from-helm-v2-to-helm-v3)

#### How to upgrade to version 10.0.0

To upgrade to _10.0.0_ from _9.x_, it should be done reusing the PVC(s) used to hold the PostgreSQL data on your previous release. To do so, follow the instructions below (the following example assumes that the release name is _postgresql_):

> NOTE: Please, create a backup of your database before running any of those actions.

1. Obtain the credentials and the names of the PVCs used to hold the PostgreSQL data on your current release:

```console
export POSTGRESQL_PASSWORD=$(kubectl get secret --namespace default postgresql -o jsonpath="{.data.postgresql-password}" | base64 --decode)
export POSTGRESQL_PVC=$(kubectl get pvc -l app.kubernetes.io/instance=postgresql,role=primary -o jsonpath="{.items[0].metadata.name}")
```

1. Delete the PostgreSQL statefulset (notice the option _--cascade=false_):

```console
kubectl delete statefulsets.apps postgresql-postgresql --namespace default --cascade=false
```

1. Upgrade your release using the same PostgreSQL version:

```console
helm upgrade postgresql bitnami/postgresql \
  --set postgresqlPassword=$POSTGRESQL_PASSWORD \
  --set persistence.existingClaim=$POSTGRESQL_PVC
```

1. Delete the existing PostgreSQL pod and the new statefulset will create a new one:

```console
kubectl delete pod postgresql-postgresql-0
```

1. Finally, you should see the lines below in PostgreSQL container logs:

```text
$ kubectl logs $(kubectl get pods -l app.kubernetes.io/instance=postgresql,app.kubernetes.io/name=postgresql,role=primary -o jsonpath="{.items[0].metadata.name}")
...
postgresql 08:05:12.59 INFO  ==> Deploying PostgreSQL with persisted data...
...
```

### To 9.0.0

In this version the chart was adapted to follow the [Helm standard labels](https://helm.sh/docs/chart_best_practices/labels/#standard-labels).

- Some inmutable objects were modified to adopt Helm standard labels introducing backward incompatibilities.

#### How to upgrade to version 9.0.0

To upgrade to _9.0.0_ from _8.x_, it should be done reusing the PVC(s) used to hold the PostgreSQL data on your previous release. To do so, follow the instructions below (the following example assumes that the release name is _postgresql_):

> NOTE: Please, create a backup of your database before running any of those actions.

1. Obtain the credentials and the names of the PVCs used to hold the PostgreSQL data on your current release:

```console
export POSTGRESQL_PASSWORD=$(kubectl get secret --namespace default postgresql -o jsonpath="{.data.postgresql-password}" | base64 --decode)
export POSTGRESQL_PVC=$(kubectl get pvc -l app=postgresql,role=master -o jsonpath="{.items[0].metadata.name}")
```

1. Delete the PostgreSQL statefulset (notice the option _--cascade=false_):

```console
kubectl delete statefulsets.apps postgresql-postgresql --namespace default --cascade=false
```

1. Upgrade your release using the same PostgreSQL version:

```console
helm upgrade postgresql bitnami/postgresql \
  --set postgresqlPassword=$POSTGRESQL_PASSWORD \
  --set persistence.existingClaim=$POSTGRESQL_PVC
```

1. Delete the existing PostgreSQL pod and the new statefulset will create a new one:

```console
kubectl delete pod postgresql-postgresql-0
```

1. Finally, you should see the lines below in PostgreSQL container logs:

```text
$ kubectl logs $(kubectl get pods -l app.kubernetes.io/instance=postgresql,app.kubernetes.io/name=postgresql,role=master -o jsonpath="{.items[0].metadata.name}")
...
postgresql 08:05:12.59 INFO  ==> Deploying PostgreSQL with persisted data...
...
```

## License

Copyright &copy; 2025 Broadcom. The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

<http://www.apache.org/licenses/LICENSE-2.0>

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

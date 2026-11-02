import { join } from 'node:path';

import * as cdk from 'aws-cdk-lib';
import * as dlm from 'aws-cdk-lib/aws-dlm';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as s3assets from 'aws-cdk-lib/aws-s3-assets';
import type { Construct } from 'constructs';

import { renderScript } from './bootstrap';
import type { SelfHostConfig } from './config';
import { resolveRetention } from './retention';

export interface SelfHostStackProps extends cdk.StackProps {
  config: SelfHostConfig;
}

/** EC2 device name for the PostgreSQL volume. */
const DATA_DEVICE = '/dev/sdf';

/**
 * Multica, self-hosted.
 *
 * One instance running the repository's own `docker-compose.selfhost.yml` —
 * PostgreSQL, the Go backend and the Next.js web app — behind Caddy, which
 * terminates TLS and forwards to the loopback ports Compose publishes. A fixed
 * Elastic IP makes the address stable, and with no domain of its own the host
 * is reached through sslip.io so Let's Encrypt can still issue a certificate.
 *
 * Every row of issue, agent and run data is in the PostgreSQL volume this
 * stack creates, in this account. The volume is separate from the instance and
 * retained when the stack is deleted, and the backend's first-party telemetry
 * is off unless `-c telemetry=true` turns it back on.
 */
export class SelfHostStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props: SelfHostStackProps) {
    super(scope, id, props);
    const { config } = props;

    const vpc = new ec2.Vpc(this, 'Vpc', {
      ipAddresses: ec2.IpAddresses.cidr('10.30.0.0/24'),
      maxAzs: 1,
      natGateways: 0,
      subnetConfiguration: [{ name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 26 }],
    });

    // 80 and 443 only. 80 is not decoration: Let's Encrypt's HTTP-01 challenge
    // arrives there, and Caddy redirects everything else to HTTPS. The app's
    // own ports (3000, 8080) stay bound to loopback inside the instance, and
    // PostgreSQL is never published at all.
    const securityGroup = new ec2.SecurityGroup(this, 'ServerSecurityGroup', {
      vpc,
      description: 'Multica self-host: HTTP and HTTPS only',
      allowAllOutbound: true,
    });
    securityGroup.addIngressRule(ec2.Peer.anyIpv4(), ec2.Port.tcp(80), 'ACME challenge and HTTPS redirect');
    securityGroup.addIngressRule(ec2.Peer.anyIpv4(), ec2.Port.tcp(443), 'Multica web and API');

    const logGroup = new logs.LogGroup(this, 'ServerLogs', {
      logGroupName: `/multica/${this.stackName}`,
      retention: resolveRetention(config.logRetentionDays),
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    const role = new iam.Role(this, 'ServerRole', {
      assumedBy: new iam.ServicePrincipal('ec2.amazonaws.com'),
      description: 'Multica self-host: own SSM parameters and own log group',
      managedPolicies: [iam.ManagedPolicy.fromAwsManagedPolicyName('AmazonSSMManagedInstanceCore')],
    });
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'ReadServerConfiguration',
        actions: ['ssm:GetParameter', 'ssm:GetParameters'],
        resources: [`arn:aws:ssm:${this.region}:${this.account}:parameter${config.parameterPrefix}/*`],
      }),
    );
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'DecryptServerConfiguration',
        actions: ['kms:Decrypt'],
        resources: ['*'],
        conditions: { StringEquals: { 'kms:ViaService': `ssm.${this.region}.amazonaws.com` } },
      }),
    );
    logGroup.grantWrite(role);
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'CloudWatchAgentBookkeeping',
        actions: ['logs:CreateLogGroup', 'logs:DescribeLogStreams'],
        resources: [logGroup.logGroupArn, `${logGroup.logGroupArn}:*`],
      }),
    );

    // The compose file ships as an asset rather than being fetched from a
    // branch at boot, so the stack runs the reviewed file and `cdk diff` shows
    // when it changes.
    const composeFile = new s3assets.Asset(this, 'ComposeFile', {
      path: join(__dirname, '..', '..', 'docker-compose.selfhost.yml'),
    });
    composeFile.grantRead(role);

    // A fixed address the team can bookmark, and — through sslip.io — the
    // hostname Caddy requests a certificate for. Allocated before the instance
    // so the bootstrap can build the URL from it.
    const address = new ec2.CfnEIP(this, 'Address', { domain: 'vpc' });

    const userData = ec2.UserData.custom(
      renderScript('selfhost-bootstrap.sh', {
        REGION: this.region,
        PARAM_PREFIX: config.parameterPrefix,
        COMPOSE_URL: composeFile.s3ObjectUrl,
        DOMAIN_NAME: config.domainName,
        PUBLIC_IP: address.ref,
        IMAGE_TAG: config.imageTag,
        ALLOWED_EMAILS: config.allowedEmails,
        // The backend reads any non-empty value as "do not send".
        DO_NOT_TRACK: config.telemetry ? '' : '1',
        LOG_GROUP: logGroup.logGroupName,
        CADDY_VERSION: config.caddyVersion,
        COMPOSE_VERSION: config.composeVersion,
        DATA_DEVICE_NAME: DATA_DEVICE,
      }),
    );

    const instance = new ec2.Instance(this, 'Server', {
      vpc,
      vpcSubnets: { subnetType: ec2.SubnetType.PUBLIC },
      instanceType: new ec2.InstanceType(config.instanceType),
      machineImage: ec2.MachineImage.latestAmazonLinux2023({ cpuType: ec2.AmazonLinuxCpuType.ARM_64 }),
      securityGroup,
      role,
      userData,
      requireImdsv2: true,
      detailedMonitoring: false,
      blockDevices: [
        {
          deviceName: '/dev/xvda',
          volume: ec2.BlockDeviceVolume.ebs(config.rootVolumeSizeGb, {
            volumeType: ec2.EbsDeviceVolumeType.GP3,
            encrypted: true,
            deleteOnTermination: true,
          }),
        },
      ],
      // Deliberately NOT userDataCausesReplacement, unlike the agent runtime:
      // replacing this instance while an EBS volume is attached to it is a way
      // to lose the database. Bootstrap changes are applied with
      // ./scripts/selfhost-apply.sh, which re-runs the script in place.
    });
    cdk.Tags.of(instance).add('Name', 'multica-self-host');

    // Separate from the instance and retained on stack delete: this is the
    // database. Deleting the stack leaves it behind to be attached to a new
    // instance or snapshotted and removed by hand.
    const dataVolume = new ec2.Volume(this, 'DataVolume', {
      availabilityZone: instance.instanceAvailabilityZone,
      size: cdk.Size.gibibytes(config.dataVolumeSizeGb),
      volumeType: ec2.EbsDeviceVolumeType.GP3,
      encrypted: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });
    cdk.Tags.of(dataVolume).add('Name', 'multica-self-host-data');
    // Tagged for the snapshot policy below to find it.
    cdk.Tags.of(dataVolume).add('Backup', 'multica-self-host');

    new ec2.CfnVolumeAttachment(this, 'DataVolumeAttachment', {
      device: DATA_DEVICE,
      instanceId: instance.instanceId,
      volumeId: dataVolume.volumeId,
    });

    new ec2.CfnEIPAssociation(this, 'AddressAssociation', {
      allocationId: address.attrAllocationId,
      instanceId: instance.instanceId,
    });

    this.addSnapshotPolicy(config, dataVolume);

    const publicHost = config.domainName || `${address.ref}.sslip.io`;
    new cdk.CfnOutput(this, 'Url', {
      value: `https://${publicHost}`,
      description: 'Open Multica here',
    });
    new cdk.CfnOutput(this, 'PublicIp', { value: address.ref, description: 'Fixed public IP' });
    new cdk.CfnOutput(this, 'InstanceId', { value: instance.instanceId, description: 'Server instance' });
    new cdk.CfnOutput(this, 'DataVolumeId', {
      value: dataVolume.volumeId,
      description: 'PostgreSQL volume, retained when the stack is deleted',
    });
    new cdk.CfnOutput(this, 'LogGroupName', { value: logGroup.logGroupName, description: 'Bootstrap logs' });
  }

  /** Daily snapshots of the database volume, through Data Lifecycle Manager. */
  private addSnapshotPolicy(config: SelfHostConfig, volume: ec2.IVolume): void {
    const role = new iam.Role(this, 'SnapshotRole', {
      assumedBy: new iam.ServicePrincipal('dlm.amazonaws.com'),
      description: 'Multica self-host: daily snapshots of the database volume',
    });
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'ManageSnapshots',
        actions: [
          'ec2:CreateSnapshot',
          'ec2:DeleteSnapshot',
          'ec2:DescribeVolumes',
          'ec2:DescribeSnapshots',
          'ec2:CreateTags',
        ],
        resources: ['*'],
      }),
    );

    const policy = new dlm.CfnLifecyclePolicy(this, 'SnapshotPolicy', {
      // DLM rejects anything outside [0-9A-Za-z _-] here, commas included.
      description: `Multica self-host database ${config.snapshotRetentionDays} daily snapshots`,
      state: 'ENABLED',
      executionRoleArn: role.roleArn,
      policyDetails: {
        resourceTypes: ['VOLUME'],
        targetTags: [{ key: 'Backup', value: 'multica-self-host' }],
        schedules: [
          {
            name: 'daily',
            createRule: { interval: 24, intervalUnit: 'HOURS', times: ['03:00'] },
            retainRule: { count: config.snapshotRetentionDays },
            copyTags: true,
          },
        ],
      },
    });
    // The policy matches by tag, so it must not be created before the volume
    // carries the tag it matches on.
    policy.node.addDependency(volume);
  }
}


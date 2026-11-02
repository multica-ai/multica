import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import type { Construct } from 'constructs';

import { renderScript } from './bootstrap';
import type { AgentRuntimeConfig } from './config';
import { resolveRetention } from './retention';

export interface AgentRuntimeStackProps extends cdk.StackProps {
  config: AgentRuntimeConfig;
}

/**
 * The shared machine Multica agents run on.
 *
 * One EC2 instance, one IAM role, one log group, and a VPC to put them in.
 * There is no load balancer, database, CDN or container registry here on
 * purpose: Multica is used as a hosted service, and this stack only supplies
 * the compute its daemon claims runs on.
 *
 * Access and credentials:
 *   - no inbound rules and no key pair; operators connect with Session Manager
 *   - the instance role can invoke Anthropic models on Bedrock and read this
 *     machine's own SSM parameters, and nothing else
 *   - the three credentials (Multica PAT, Claude OAuth token, GitHub token)
 *     are SecureString parameters created outside CloudFormation, so no secret
 *     value ever enters a template, a change set or the CDK context
 */
export class AgentRuntimeStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props: AgentRuntimeStackProps) {
    super(scope, id, props);
    const { config } = props;

    // One AZ. The box holds no state worth preserving — a replacement
    // re-checks-out every repository — so paying for a second subnet's worth of
    // redundancy buys nothing.
    const vpc = new ec2.Vpc(this, 'Vpc', {
      ipAddresses: ec2.IpAddresses.cidr('10.20.0.0/24'),
      maxAzs: 1,
      natGateways: config.natGateway ? 1 : 0,
      subnetConfiguration: config.natGateway
        ? [
            { name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 26 },
            { name: 'private', subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS, cidrMask: 26 },
          ]
        : [{ name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 26 }],
    });

    // Egress only. The machine listens on nothing: the daemon dials out to
    // Multica over WebSocket and Session Manager dials out to SSM, so there is
    // no ingress rule to write — not even one for SSH.
    const securityGroup = new ec2.SecurityGroup(this, 'RuntimeSecurityGroup', {
      vpc,
      description: 'Multica agent runtime: outbound only, no listeners',
      allowAllOutbound: true,
    });

    const logGroup = new logs.LogGroup(this, 'RuntimeLogs', {
      logGroupName: `/multica/${this.stackName}`,
      retention: resolveRetention(config.logRetentionDays),
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    const role = new iam.Role(this, 'RuntimeRole', {
      assumedBy: new iam.ServicePrincipal('ec2.amazonaws.com'),
      description: 'Multica agent runtime: Bedrock inference, own SSM parameters, own log group',
      managedPolicies: [
        // Session Manager. This is the only way in, so it is also the only
        // place to audit access from.
        iam.ManagedPolicy.fromAwsManagedPolicyName('AmazonSSMManagedInstanceCore'),
      ],
    });

    // The fallback runtime's entire AWS surface: invoke Anthropic models.
    // Foundation-model ARNs stay region-wildcarded because a cross-region
    // inference profile routes a request to whichever region has capacity, and
    // the caller needs invoke rights on the model in each of them.
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'InvokeAnthropicOnBedrock',
        actions: ['bedrock:InvokeModel', 'bedrock:InvokeModelWithResponseStream', 'bedrock:CountTokens'],
        resources: [
          'arn:aws:bedrock:*::foundation-model/anthropic.*',
          `arn:aws:bedrock:*:${this.account}:inference-profile/*anthropic.*`,
        ],
      }),
    );
    // Model discovery for the runtime's model picker. Read-only and listing
    // only, which is why it is a separate statement from the invoke grant.
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'DiscoverBedrockModels',
        actions: [
          'bedrock:ListFoundationModels',
          'bedrock:GetFoundationModel',
          'bedrock:ListInferenceProfiles',
          'bedrock:GetInferenceProfile',
        ],
        resources: ['*'],
      }),
    );

    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'ReadRuntimeCredentials',
        actions: ['ssm:GetParameter', 'ssm:GetParameters'],
        resources: [
          `arn:aws:ssm:${this.region}:${this.account}:parameter${config.parameterPrefix}/*`,
        ],
      }),
    );
    // SecureString parameters are encrypted with the account's aws/ssm key.
    // The ViaService condition keeps this from becoming a general decrypt
    // grant: the role can only use the key through Parameter Store.
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'DecryptRuntimeCredentials',
        actions: ['kms:Decrypt'],
        resources: ['*'],
        conditions: { StringEquals: { 'kms:ViaService': `ssm.${this.region}.amazonaws.com` } },
      }),
    );

    logGroup.grantWrite(role);
    // The CloudWatch agent calls CreateLogGroup and DescribeLogStreams on
    // start, before it writes anything. Without these it logs an access error
    // and ships nothing — the gap the previous revision shipped with.
    role.addToPolicy(
      new iam.PolicyStatement({
        sid: 'CloudWatchAgentBookkeeping',
        actions: ['logs:CreateLogGroup', 'logs:DescribeLogStreams'],
        resources: [logGroup.logGroupArn, `${logGroup.logGroupArn}:*`],
      }),
    );

    const userData = ec2.UserData.custom(
      renderScript('bootstrap.sh', {
        REGION: this.region,
        PARAM_PREFIX: config.parameterPrefix,
        SERVER_URL: config.serverUrl,
        CLI_VERSION: config.cliVersion,
        NODE_MAJOR: config.nodeMajorVersion,
        LOG_GROUP: logGroup.logGroupName,
        MAX_CONCURRENT_TASKS: String(config.maxConcurrentTasks),
        DAEMON_ID: config.daemonId,
      }),
    );

    const instance = new ec2.Instance(this, 'Runtime', {
      vpc,
      vpcSubnets: {
        subnetType: config.natGateway ? ec2.SubnetType.PRIVATE_WITH_EGRESS : ec2.SubnetType.PUBLIC,
      },
      instanceType: new ec2.InstanceType(config.instanceType),
      machineImage: ec2.MachineImage.latestAmazonLinux2023({
        cpuType: ec2.AmazonLinuxCpuType.ARM_64,
      }),
      securityGroup,
      role,
      userData,
      // Bootstrap changes are the whole configuration of this machine, so they
      // have to reach it. Without this a user data edit updates the launch
      // template and nothing else.
      userDataCausesReplacement: true,
      requireImdsv2: true,
      detailedMonitoring: false,
      blockDevices: [
        {
          deviceName: '/dev/xvda',
          volume: ec2.BlockDeviceVolume.ebs(config.volumeSizeGb, {
            volumeType: ec2.EbsDeviceVolumeType.GP3,
            encrypted: true,
            deleteOnTermination: true,
          }),
        },
      ],
    });
    cdk.Tags.of(instance).add('Name', 'multica-agent-runtime');

    // Outputs carry no exportName: nothing imports them, and an unnamespaced
    // export is what collides with the next stack in the account.
    new cdk.CfnOutput(this, 'InstanceId', {
      value: instance.instanceId,
      description: 'Agent runtime instance',
    });
    new cdk.CfnOutput(this, 'ConnectCommand', {
      value: `aws ssm start-session --region ${this.region} --target ${instance.instanceId}`,
      description: 'Open a shell on the runtime machine',
    });
    new cdk.CfnOutput(this, 'LogGroupName', {
      value: logGroup.logGroupName,
      description: 'Daemon and bootstrap logs',
    });
    new cdk.CfnOutput(this, 'ParameterPrefix', {
      value: config.parameterPrefix,
      description: 'SSM prefix the runtime reads its credentials from',
    });
  }
}


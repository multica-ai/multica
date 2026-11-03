/**
 * Configuration for the Multica agent runtime machine.
 *
 * There is one environment on purpose. Multica itself runs on
 * https://multica.ai, so nothing here hosts the web app, the Go server, or a
 * database — this stack only provides the Linux box that runs the Multica
 * daemon and the Claude Code runtimes it registers.
 *
 * Every value is overridable through CDK context so a deploy can change the
 * shape of the machine without a code change:
 *
 *   cdk deploy -c instanceType=t4g.large -c natGateway=true
 */
import type { App } from 'aws-cdk-lib';

export interface AgentRuntimeConfig {
  /** EC2 instance type. Graviton only — the AMI is arm64. */
  instanceType: string;
  /** Root EBS volume size in GiB. Git checkouts plus node_modules add up. */
  volumeSizeGb: number;
  /**
   * When true the instance moves to a private subnet behind a NAT gateway.
   * Default false: the instance sits in a public subnet with no inbound rules
   * and reaches the internet directly, which saves the NAT gateway's ~$33/mo.
   * Either way there is no SSH and no listener — access is Session Manager.
   */
  natGateway: boolean;
  /** Multica API the daemon logs in to. */
  serverUrl: string;
  /** SSM Parameter Store prefix holding the runtime's credentials. */
  parameterPrefix: string;
  /** Multica CLI release tag, or 'latest' to resolve the newest release. */
  cliVersion: string;
  /** Node.js major version Claude Code runs on. */
  nodeMajorVersion: string;
  /** CloudWatch Logs retention in days. */
  logRetentionDays: number;
  /** MULTICA_DAEMON_MAX_CONCURRENT_TASKS for the daemon. */
  maxConcurrentTasks: number;
  /**
   * MULTICA_DAEMON_ID for the daemon. The server keys runtimes by daemon id,
   * and without a pin the daemon mints a random one on first boot, so every
   * instance replacement would register new, private runtimes and orphan the
   * public ones agents are bound to. Empty leaves the daemon's own id.
   */
  daemonId: string;
}

const defaults: AgentRuntimeConfig = {
  instanceType: 't4g.medium',
  volumeSizeGb: 40,
  natGateway: false,
  serverUrl: 'https://api.multica.ai',
  parameterPrefix: '/multica/agent-runtime',
  cliVersion: 'latest',
  nodeMajorVersion: '22',
  logRetentionDays: 14,
  maxConcurrentTasks: 4,
  daemonId: '',
};

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/**
 * Graviton instance families. The machine image is arm64, so an x86 instance
 * type would boot the wrong AMI architecture and fail at launch with a message
 * that does not say why. Reject it at synth instead.
 */
const GRAVITON_FAMILY = /^[a-z]+\d+g[a-z]*$/;

export function resolveConfig(app: App): AgentRuntimeConfig {
  const config: AgentRuntimeConfig = {
    instanceType: contextString(app, 'instanceType', defaults.instanceType),
    volumeSizeGb: contextNumber(app, 'volumeSizeGb', defaults.volumeSizeGb),
    natGateway: contextBoolean(app, 'natGateway', defaults.natGateway),
    serverUrl: contextString(app, 'serverUrl', defaults.serverUrl),
    parameterPrefix: contextString(app, 'parameterPrefix', defaults.parameterPrefix),
    cliVersion: contextString(app, 'cliVersion', defaults.cliVersion),
    nodeMajorVersion: contextString(app, 'nodeMajorVersion', defaults.nodeMajorVersion),
    logRetentionDays: contextNumber(app, 'logRetentionDays', defaults.logRetentionDays),
    maxConcurrentTasks: contextNumber(app, 'maxConcurrentTasks', defaults.maxConcurrentTasks),
    daemonId: contextString(app, 'daemonId', defaults.daemonId),
  };

  const family = config.instanceType.split('.')[0] ?? '';
  if (!GRAVITON_FAMILY.test(family)) {
    throw new Error(
      `instanceType ${config.instanceType} is not a Graviton (arm64) type; the machine image is arm64. Use t4g/m7g/c7g/r7g or similar.`,
    );
  }
  if (!config.parameterPrefix.startsWith('/') || config.parameterPrefix.endsWith('/')) {
    throw new Error(`parameterPrefix must start with "/" and not end with one, got ${config.parameterPrefix}`);
  }
  if (!/^https:\/\//.test(config.serverUrl)) {
    throw new Error(`serverUrl must be https, got ${config.serverUrl}`);
  }
  if (config.volumeSizeGb < 20) {
    throw new Error(`volumeSizeGb must be at least 20, got ${config.volumeSizeGb}`);
  }
  if (config.maxConcurrentTasks < 1) {
    throw new Error(`maxConcurrentTasks must be at least 1, got ${config.maxConcurrentTasks}`);
  }
  if (config.daemonId !== '' && !UUID.test(config.daemonId)) {
    throw new Error(`daemonId must be a lowercase UUID, got ${config.daemonId}`);
  }
  return config;
}

function contextString(app: App, key: string, fallback: string): string {
  const value = app.node.tryGetContext(key);
  return value === undefined ? fallback : String(value);
}

function contextNumber(app: App, key: string, fallback: number): number {
  const value = app.node.tryGetContext(key);
  if (value === undefined) {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error(`context ${key} must be a number, got ${String(value)}`);
  }
  return parsed;
}

function contextBoolean(app: App, key: string, fallback: boolean): boolean {
  const value = app.node.tryGetContext(key);
  if (value === undefined) {
    return fallback;
  }
  // `-c natGateway=true` arrives as the string "true".
  return value === true || value === 'true' || value === '1';
}

/**
 * Configuration for the self-hosted Multica server.
 *
 * This is the stack that runs Multica itself — the Go backend, the Next.js
 * web app and PostgreSQL — from the repository's own
 * `docker-compose.selfhost.yml`, behind Caddy on a fixed public IP. All issue,
 * agent and run data lives in the PostgreSQL volume this stack creates, in
 * this AWS account.
 */
export interface SelfHostConfig {
  instanceType: string;
  /** Root volume in GiB: OS plus the three container images. */
  rootVolumeSizeGb: number;
  /** PostgreSQL volume in GiB. A separate volume, retained on stack delete. */
  dataVolumeSizeGb: number;
  /** Daily snapshots of the data volume to keep. */
  snapshotRetentionDays: number;
  /**
   * Public hostname. Empty means derive it from the Elastic IP through
   * sslip.io, which resolves <ip>.sslip.io to <ip> and so lets Caddy issue a
   * real certificate without owning a domain.
   */
  domainName: string;
  /** Multica image tag from GHCR. */
  imageTag: string;
  /** Who may create an account. Required: this host is on the open internet. */
  allowedEmails: string;
  /**
   * First-party deployment telemetry to telemetry.multica.ai. Off by default:
   * a self-host exists so the data stays here.
   */
  telemetry: boolean;
  /**
   * Existing VPC to deploy into, with the public subnet and availability zone
   * to place the instance in. All three together, or none.
   *
   * An account at its VPC or internet-gateway limit cannot create another one,
   * and those limits are per region and slow to raise. Reusing a VPC keeps the
   * server a separate instance with its own security group — the machines stay
   * isolated from each other by their rules, not by having separate networks.
   */
  vpcId: string;
  subnetId: string;
  availabilityZone: string;
  /**
   * Allocate an Elastic IP. False uses the instance's auto-assigned address,
   * for an account at its Elastic IP limit. That address survives a reboot but
   * not a stop/start, and the hostname and certificate are derived from it, so
   * a move needs ./scripts/selfhost-apply.sh to re-issue them.
   */
  elasticIp: boolean;
  parameterPrefix: string;
  logRetentionDays: number;
  caddyVersion: string;
  composeVersion: string;
}

const selfHostDefaults: SelfHostConfig = {
  instanceType: 't4g.medium',
  rootVolumeSizeGb: 30,
  dataVolumeSizeGb: 20,
  snapshotRetentionDays: 7,
  domainName: '',
  imageTag: 'latest',
  allowedEmails: '',
  telemetry: false,
  vpcId: '',
  subnetId: '',
  availabilityZone: '',
  elasticIp: true,
  parameterPrefix: '/multica/self-host',
  logRetentionDays: 14,
  caddyVersion: '2.10.0',
  composeVersion: 'v2.32.4',
};

export function resolveSelfHostConfig(app: App): SelfHostConfig {
  const config: SelfHostConfig = {
    instanceType: contextString(app, 'selfHostInstanceType', selfHostDefaults.instanceType),
    rootVolumeSizeGb: contextNumber(app, 'rootVolumeSizeGb', selfHostDefaults.rootVolumeSizeGb),
    dataVolumeSizeGb: contextNumber(app, 'dataVolumeSizeGb', selfHostDefaults.dataVolumeSizeGb),
    snapshotRetentionDays: contextNumber(app, 'snapshotRetentionDays', selfHostDefaults.snapshotRetentionDays),
    domainName: contextString(app, 'domainName', selfHostDefaults.domainName),
    imageTag: contextString(app, 'imageTag', selfHostDefaults.imageTag),
    allowedEmails: contextString(app, 'allowedEmails', selfHostDefaults.allowedEmails),
    telemetry: contextBoolean(app, 'telemetry', selfHostDefaults.telemetry),
    vpcId: contextString(app, 'vpcId', selfHostDefaults.vpcId),
    subnetId: contextString(app, 'subnetId', selfHostDefaults.subnetId),
    availabilityZone: contextString(app, 'availabilityZone', selfHostDefaults.availabilityZone),
    elasticIp: contextBoolean(app, 'elasticIp', selfHostDefaults.elasticIp),
    parameterPrefix: contextString(app, 'selfHostParameterPrefix', selfHostDefaults.parameterPrefix),
    logRetentionDays: contextNumber(app, 'logRetentionDays', selfHostDefaults.logRetentionDays),
    caddyVersion: contextString(app, 'caddyVersion', selfHostDefaults.caddyVersion),
    composeVersion: contextString(app, 'composeVersion', selfHostDefaults.composeVersion),
  };

  const family = config.instanceType.split('.')[0] ?? '';
  if (!GRAVITON_FAMILY.test(family)) {
    throw new Error(
      `selfHostInstanceType ${config.instanceType} is not a Graviton (arm64) type; the machine image is arm64.`,
    );
  }
  // Port 443 is open to the internet, so an open signup form would let anyone
  // who finds the URL create an account in the workspace.
  if (!config.allowedEmails.trim()) {
    throw new Error(
      'allowedEmails is required: this server is reachable from the internet, and without an allowlist anyone who finds the URL can sign up. Pass -c allowedEmails=you@example.com,teammate@example.com',
    );
  }
  // Importing a VPC needs all three: the subnet decides placement and the zone
  // is not discoverable from an id at synth, so a missing one would land the
  // instance in a zone CloudFormation picks and the data volume in another.
  const network = [config.vpcId, config.subnetId, config.availabilityZone];
  if (network.some(Boolean) && !network.every(Boolean)) {
    throw new Error(
      'vpcId, subnetId and availabilityZone go together: pass all three to reuse an existing network, or none to create one.',
    );
  }
  if (config.vpcId && !/^vpc-[0-9a-f]+$/.test(config.vpcId)) {
    throw new Error(`vpcId must look like vpc-0123abc, got ${config.vpcId}`);
  }
  if (config.subnetId && !/^subnet-[0-9a-f]+$/.test(config.subnetId)) {
    throw new Error(`subnetId must look like subnet-0123abc, got ${config.subnetId}`);
  }
  if (config.domainName && !/^[a-z0-9.-]+$/i.test(config.domainName)) {
    throw new Error(`domainName ${config.domainName} is not a hostname`);
  }
  if (!config.parameterPrefix.startsWith('/') || config.parameterPrefix.endsWith('/')) {
    throw new Error(`selfHostParameterPrefix must start with "/" and not end with one, got ${config.parameterPrefix}`);
  }
  if (config.rootVolumeSizeGb < 20) {
    throw new Error(`rootVolumeSizeGb must be at least 20, got ${config.rootVolumeSizeGb}`);
  }
  if (config.dataVolumeSizeGb < 10) {
    throw new Error(`dataVolumeSizeGb must be at least 10, got ${config.dataVolumeSizeGb}`);
  }
  return config;
}

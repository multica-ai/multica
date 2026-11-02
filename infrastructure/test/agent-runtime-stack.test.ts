// @vitest-environment node
import { App } from 'aws-cdk-lib';
import { Match, Template } from 'aws-cdk-lib/assertions';
import { describe, expect, it } from 'vitest';

import cdkJson from '../cdk.json';
import { AgentRuntimeStack } from '../lib/agent-runtime-stack';
import { resolveConfig } from '../lib/config';

// Synthesizing is the expensive part of this file — seconds for the first one
// on a cold runner, because it stages the asset behind the default-security-
// group custom resource. Every case below asserts against one of four
// configurations, so each is built once and shared.
const templates = new Map<string, Template>();

function synth(context: Record<string, string> = {}): { template: Template } {
  const key = JSON.stringify(context);
  const cached = templates.get(key);
  if (cached) {
    return { template: cached };
  }

  // cdk.json's feature flags decide what the deployed template looks like, so
  // a test that skips them asserts against a template nobody deploys.
  const app = new App({ context: { ...cdkJson.context, ...context } });
  const stack = new AgentRuntimeStack(app, 'Multica-AgentRuntime', {
    env: { account: '123456789012', region: 'ap-south-1' },
    config: resolveConfig(app),
  });

  // Nothing is cached for a configuration that fails validation: those throw
  // before this line, and re-running them is cheap.
  const template = Template.fromStack(stack);
  templates.set(key, template);
  return { template };
}

function userData(template: Template): string {
  const instances = template.findResources('AWS::EC2::Instance');
  const resource = Object.values(instances)[0];
  // User data renders as Fn::Base64 over an Fn::Join of literals and
  // intrinsics; the literals are all we need to assert on.
  const joined = resource?.Properties?.UserData?.['Fn::Base64']?.['Fn::Join']?.[1] ?? [];
  return (joined as unknown[]).filter((part): part is string => typeof part === 'string').join('');
}

describe('config', () => {
  it('rejects an x86 instance type, which would not boot the arm64 image', () => {
    expect(() => synth({ instanceType: 't3.small' })).toThrow(/not a Graviton/);
  });

  it('rejects a parameter prefix that is not an absolute SSM path', () => {
    expect(() => synth({ parameterPrefix: 'multica/agent-runtime' })).toThrow(/must start with/);
  });

  it('rejects a daemon id that is not a UUID', () => {
    expect(() => synth({ daemonId: 'multica-agent-runtime' })).toThrow(/must be a lowercase UUID/);
  });

  it('rejects a retention period CloudWatch does not accept', () => {
    expect(() => synth({ logRetentionDays: '13' })).toThrow(/not a CloudWatch retention period/);
  });
});

describe('network', () => {
  it('opens no inbound port at all', () => {
    const { template } = synth();
    const groups = template.findResources('AWS::EC2::SecurityGroup');
    for (const group of Object.values(groups)) {
      expect(group.Properties?.SecurityGroupIngress).toBeUndefined();
    }
    expect(Object.keys(template.findResources('AWS::EC2::SecurityGroupIngress'))).toHaveLength(0);
  });

  it('creates exactly one security group, used by the instance', () => {
    const { template } = synth();
    // The previous revision declared three security groups in a network stack
    // that nothing used, plus one more per stack that did.
    expect(Object.keys(template.findResources('AWS::EC2::SecurityGroup'))).toHaveLength(1);
    template.hasResourceProperties('AWS::EC2::Instance', {
      SecurityGroupIds: Match.anyValue(),
    });
  });

  it('skips the NAT gateway by default', () => {
    const { template } = synth();
    expect(Object.keys(template.findResources('AWS::EC2::NatGateway'))).toHaveLength(0);
  });

  it('adds a NAT gateway and a private subnet when asked', () => {
    const { template } = synth({ natGateway: 'true' });
    expect(Object.keys(template.findResources('AWS::EC2::NatGateway'))).toHaveLength(1);
    template.hasResourceProperties('AWS::EC2::Subnet', {
      MapPublicIpOnLaunch: false,
    });
  });

  it('attaches no key pair, so there is no SSH path onto the box', () => {
    const { template } = synth();
    const instances = Object.values(template.findResources('AWS::EC2::Instance'));
    expect(instances).toHaveLength(1);
    expect(instances[0]?.Properties?.KeyName).toBeUndefined();
  });
});

describe('instance role', () => {
  it('grants Bedrock invoke only on Anthropic models', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['bedrock:InvokeModel', 'bedrock:InvokeModelWithResponseStream']),
            Resource: Match.arrayWith(['arn:aws:bedrock:*::foundation-model/anthropic.*']),
          }),
        ]),
      }),
    });
  });

  it('reaches no service beyond Bedrock, Parameter Store and its own log group', () => {
    const { template } = synth();
    const inline = Object.entries(template.findResources('AWS::IAM::Policy')).filter(([id]) =>
      id.startsWith('RuntimeRoleDefaultPolicy'),
    );
    expect(inline).toHaveLength(1);

    const services = new Set<string>();
    for (const statement of inline[0]![1].Properties?.PolicyDocument?.Statement ?? []) {
      for (const action of [statement.Action].flat()) {
        if (typeof action === 'string') {
          services.add(action.split(':')[0]!);
        }
      }
    }
    expect([...services].sort()).toEqual(['bedrock', 'kms', 'logs', 'ssm']);
  });

  it('scopes the SSM read to the runtime parameter prefix', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['ssm:GetParameter']),
            Resource: 'arn:aws:ssm:ap-south-1:123456789012:parameter/multica/agent-runtime/*',
          }),
        ]),
      }),
    });
  });

  it('confines the KMS decrypt grant to Parameter Store', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: 'kms:Decrypt',
            Condition: { StringEquals: { 'kms:ViaService': 'ssm.ap-south-1.amazonaws.com' } },
          }),
        ]),
      }),
    });
  });

  it('attaches Session Manager access', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::IAM::Role', {
      ManagedPolicyArns: Match.arrayWith([
        Match.objectLike({
          'Fn::Join': Match.arrayWith([
            Match.arrayWith([':iam::aws:policy/AmazonSSMManagedInstanceCore']),
          ]),
        }),
      ]),
    });
  });
});

describe('instance', () => {
  it('encrypts the root volume', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::EC2::Instance', {
      BlockDeviceMappings: Match.arrayWith([
        Match.objectLike({ Ebs: Match.objectLike({ Encrypted: true, VolumeType: 'gp3' }) }),
      ]),
    });
  });

  it('requires IMDSv2', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::EC2::LaunchTemplate', {
      LaunchTemplateData: Match.objectLike({
        MetadataOptions: Match.objectLike({ HttpTokens: 'required' }),
      }),
    });
  });
});

describe('bootstrap', () => {
  it('installs both runtime wrappers and starts the daemon service', () => {
    const script = userData(synth().template);
    expect(script).toContain('/usr/local/bin/claude-max');
    expect(script).toContain('/usr/local/bin/claude-bedrock');
    expect(script).toContain('multica daemon start --foreground');
    expect(script).toContain('systemctl enable --now multica-daemon.service');
  });

  it('runs the daemon as a non-root user', () => {
    const script = userData(synth().template);
    expect(script).toContain('User=multica');
    expect(script).not.toContain('User=root');
  });

  it('creates the daemon log directory before the unit that appends to it', () => {
    // systemd opens StandardOutput=append: before running the root ("+")
    // ExecStartPre, and LogsDirectory= is not set up for that step. Without
    // the directory the unit fails with 209/STDOUT on every start.
    const script = userData(synth().template);
    const mkdir = script.indexOf('install -d -m 0750 -o "${RUNTIME_USER}" -g "${RUNTIME_USER}" /var/log/multica');
    expect(mkdir).toBeGreaterThan(-1);
    expect(mkdir).toBeLessThan(script.indexOf('systemctl enable --now multica-daemon.service'));
  });

  it('ships the log the daemon itself writes, not only the unit output', () => {
    // Without a terminal the CLI routes its logger to ~/.multica/daemon.log;
    // the unit's stdout only ever sees the login step.
    const script = userData(synth().template);
    expect(script).toContain('"file_path": "${RUNTIME_HOME}/.multica/daemon.log"');
    expect(script).toContain('"file_path": "/var/log/multica/daemon.log"');
  });

  it('pins the daemon id, so a replacement instance keeps the same runtimes', () => {
    // A deployment passes its own id with -c daemonId=<uuid>; the value is not
    // committed, because it names one particular installation's runtimes.
    const daemonId = '3f1b4f4e-8b2a-4a1e-9f3c-2d5e6a7b8c9d';
    expect(userData(synth({ daemonId }).template)).toContain(
      `Environment=MULTICA_DAEMON_ID=${daemonId}`,
    );
  });

  it('leaves the daemon id empty when none is given, so the daemon mints one', () => {
    expect(userData(synth().template)).toMatch(/^Environment=MULTICA_DAEMON_ID=$/m);
  });

  it('substitutes every placeholder', () => {
    expect(userData(synth().template)).not.toContain('@@');
  });

  it('keeps credentials out of the root volume and out of argv', () => {
    const script = userData(synth().template);
    // Tokens are read from tmpfs at use time...
    expect(script).toContain('/run/multica/claude-oauth-token');
    expect(script).toContain('install -d -m 0750 -o "${RUNTIME_USER}"');
    // ...the PAT reaches `multica login` on stdin, not as an argument...
    expect(script).toContain('multica login --token </run/multica/multica-token');
    // ...and git reads its token through a helper, so nothing writes it to a
    // credential file on disk.
    expect(script).toContain('credential.https://github.com.helper');
    expect(script).not.toMatch(/>\s*\S*\.git-credentials/);
  });

  it('carries the configured server URL and concurrency into the unit file', () => {
    const script = userData(synth({ serverUrl: 'https://multica.example.com', maxConcurrentTasks: '2' }).template);
    expect(script).toContain('SERVER_URL="https://multica.example.com"');
    expect(script).toContain('MULTICA_DAEMON_MAX_CONCURRENT_TASKS=2');
  });
});

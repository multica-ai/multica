// @vitest-environment node
import { App } from 'aws-cdk-lib';
import { Match, Template } from 'aws-cdk-lib/assertions';
import { describe, expect, it } from 'vitest';

import cdkJson from '../cdk.json';
import { resolveSelfHostConfig } from '../lib/config';
import { SelfHostStack } from '../lib/self-host-stack';

const baseContext = { allowedEmails: 'owner@example.com' };

// One synth per configuration, shared across cases. See the note in
// agent-runtime-stack.test.ts: the first one is slow enough to matter.
const templates = new Map<string, Template>();

function synth(context: Record<string, string> = {}): { template: Template } {
  const merged = { ...baseContext, ...context };
  const key = JSON.stringify(merged);
  const cached = templates.get(key);
  if (cached) {
    return { template: cached };
  }

  const app = new App({ context: { ...cdkJson.context, ...merged } });
  const stack = new SelfHostStack(app, 'Multica-SelfHost', {
    env: { account: '123456789012', region: 'ap-south-1' },
    config: resolveSelfHostConfig(app),
  });
  const template = Template.fromStack(stack);
  templates.set(key, template);
  return { template };
}

function userData(template: Template): string {
  const resource = Object.values(template.findResources('AWS::EC2::Instance'))[0];
  const joined = resource?.Properties?.UserData?.['Fn::Base64']?.['Fn::Join']?.[1] ?? [];
  return (joined as unknown[]).filter((part): part is string => typeof part === 'string').join('');
}

describe('config', () => {
  it('refuses to build without an email allowlist', () => {
    const app = new App({ context: cdkJson.context });
    expect(() => resolveSelfHostConfig(app)).toThrow(/allowedEmails is required/);
  });

  it('rejects an x86 instance type', () => {
    expect(() => synth({ selfHostInstanceType: 'm5.large' })).toThrow(/not a Graviton/);
  });

  it('rejects a domain that is not a hostname', () => {
    expect(() => synth({ domainName: 'https://multica.example.com/' })).toThrow(/is not a hostname/);
  });
});

describe('network', () => {
  it('opens only 80 and 443', () => {
    const { template } = synth();
    const groups = Object.values(template.findResources('AWS::EC2::SecurityGroup'));
    expect(groups).toHaveLength(1);
    const ports = (groups[0]?.Properties?.SecurityGroupIngress ?? []).map(
      (rule: { FromPort: number; ToPort: number; CidrIp: string }) => [rule.FromPort, rule.ToPort, rule.CidrIp],
    );
    expect(ports).toEqual([
      [80, 80, '0.0.0.0/0'],
      [443, 443, '0.0.0.0/0'],
    ]);
  });

  it('never exposes PostgreSQL or the raw app ports', () => {
    const { template } = synth();
    const groups = Object.values(template.findResources('AWS::EC2::SecurityGroup'));
    const ports = (groups[0]?.Properties?.SecurityGroupIngress ?? []).map(
      (rule: { FromPort: number }) => rule.FromPort,
    );
    expect(ports).not.toContain(5432);
    expect(ports).not.toContain(3000);
    expect(ports).not.toContain(8080);
    expect(ports).not.toContain(22);
  });

  it('gives the server a fixed public address', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::EC2::EIP', { Domain: 'vpc' });
    expect(Object.keys(template.findResources('AWS::EC2::EIPAssociation'))).toHaveLength(1);
  });
});

describe('the database volume', () => {
  it('is encrypted, separate from the instance, and survives stack deletion', () => {
    const { template } = synth();
    const volumes = Object.values(template.findResources('AWS::EC2::Volume'));
    expect(volumes).toHaveLength(1);
    expect(volumes[0]?.Properties?.Encrypted).toBe(true);
    // Losing this is losing every issue, comment and run in the deployment.
    expect(volumes[0]?.DeletionPolicy).toBe('Retain');
    expect(volumes[0]?.UpdateReplacePolicy).toBe('Retain');
  });

  it('is attached rather than part of the instance', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::EC2::VolumeAttachment', { Device: '/dev/sdf' });
    const instance = Object.values(template.findResources('AWS::EC2::Instance'))[0];
    const devices = (instance?.Properties?.BlockDeviceMappings ?? []).map(
      (mapping: { DeviceName: string }) => mapping.DeviceName,
    );
    expect(devices).toEqual(['/dev/xvda']);
  });

  it('is snapshotted daily', () => {
    const { template } = synth();
    template.hasResourceProperties('AWS::DLM::LifecyclePolicy', {
      State: 'ENABLED',
      PolicyDetails: Match.objectLike({
        ResourceTypes: ['VOLUME'],
        TargetTags: [{ Key: 'Backup', Value: 'multica-self-host' }],
        Schedules: [
          Match.objectLike({
            CreateRule: Match.objectLike({ Interval: 24, IntervalUnit: 'HOURS' }),
            RetainRule: { Count: 7 },
          }),
        ],
      }),
    });
  });
});

describe('bootstrap', () => {
  it('keeps the data on this deployment: telemetry off by default', () => {
    expect(userData(synth().template)).toContain('DO_NOT_TRACK_VALUE="1"');
  });

  it('sends telemetry only when explicitly asked to', () => {
    expect(userData(synth({ telemetry: 'true' }).template)).toContain('DO_NOT_TRACK_VALUE=""');
  });

  it('carries the signup allowlist into the backend configuration', () => {
    const script = userData(synth({ allowedEmails: 'a@example.com,b@example.com' }).template);
    expect(script).toContain('ALLOWED_EMAILS="a@example.com,b@example.com"');
    expect(script).toContain('ALLOWED_EMAILS=${ALLOWED_EMAILS}');
  });

  it('never sets the fixed development verification code', () => {
    // The self-hosting guide warns that this lets anyone who knows an email
    // address log in, which on an internet-facing host is the whole account.
    expect(userData(synth().template)).not.toContain('MULTICA_DEV_VERIFICATION_CODE');
  });

  it('routes WebSockets to the backend ahead of the catch-all', () => {
    const script = userData(synth().template);
    const wsMatcher = script.indexOf('@multica_ws path /ws /ws/*');
    const catchAll = script.indexOf('reverse_proxy localhost:3000');
    expect(wsMatcher).toBeGreaterThan(-1);
    expect(catchAll).toBeGreaterThan(wsMatcher);
    expect(script).toContain('flush_interval -1');
  });

  it('tells the backend which origin to accept, or the WebSocket upgrade is refused', () => {
    const script = userData(synth({ domainName: 'multica.example.com' }).template);
    expect(script).toContain('DOMAIN_NAME="multica.example.com"');
    expect(script).toContain('FRONTEND_ORIGIN=${PUBLIC_URL}');
    expect(script).toContain('CORS_ALLOWED_ORIGINS=${PUBLIC_URL}');
  });

  it('puts PostgreSQL on the attached volume, not in a Docker volume on the root disk', () => {
    const script = userData(synth().template);
    expect(script).toContain('DATA_DIR="/var/lib/multica"');
    expect(script).toContain('${DATA_DIR}/postgres:/var/lib/postgresql/data');
    expect(script).toContain('${DATA_DIR}/uploads:/app/data/uploads');
  });

  it('writes the env file unreadable to other users', () => {
    const script = userData(synth().template);
    expect(script).toContain('chmod 0600 "${APP_DIR}/.env"');
  });

  it('substitutes every placeholder', () => {
    expect(userData(synth().template)).not.toMatch(/@@\w+@@/);
  });
});

describe('instance role', () => {
  it('reaches no service beyond Parameter Store, its log group and the asset bucket', () => {
    const { template } = synth();
    const inline = Object.entries(template.findResources('AWS::IAM::Policy')).filter(([id]) =>
      id.startsWith('ServerRoleDefaultPolicy'),
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
    expect([...services].sort()).toEqual(['kms', 'logs', 's3', 'ssm']);
  });
});

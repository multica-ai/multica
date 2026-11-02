#!/usr/bin/env node
import * as cdk from 'aws-cdk-lib';

import { AgentRuntimeStack } from '../lib/agent-runtime-stack';
import { resolveConfig, resolveSelfHostConfig } from '../lib/config';
import { SelfHostStack } from '../lib/self-host-stack';

const app = new cdk.App();

// Region and account come from the ambient AWS configuration. Both stay
// undefined when no credentials are present, which is what lets `cdk synth`
// run in CI: the template is then region-agnostic.
const env = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION ?? process.env.AWS_REGION,
};

// The two stacks are independent and deploy separately. The runtime machine
// can point at multica.ai or at the self-hosted server through its serverUrl
// context value; nothing in CloudFormation ties them together, so neither one
// has to exist for the other to work.
const stacks: cdk.Stack[] = [
  new AgentRuntimeStack(app, 'Multica-AgentRuntime', {
    env,
    description: 'Shared machine that runs the Multica agent daemon and its Claude Code runtimes',
    config: resolveConfig(app),
  }),
];

// Self-hosting is opt-in: the stack requires an email allowlist, so building
// it unconditionally would fail every synth that only wants the agent runtime.
if (app.node.tryGetContext('selfHost') !== undefined) {
  stacks.push(
    new SelfHostStack(app, 'Multica-SelfHost', {
      env,
      description: 'Self-hosted Multica: web, API and PostgreSQL behind Caddy on a fixed public IP',
      config: resolveSelfHostConfig(app),
    }),
  );
}

for (const stack of stacks) {
  cdk.Tags.of(stack).add('Application', 'Multica');
  cdk.Tags.of(stack).add('ManagedBy', 'CDK');
  cdk.Tags.of(stack).add('Component', stack.stackName.replace('Multica-', ''));
}

app.synth();

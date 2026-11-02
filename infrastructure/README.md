# Infrastructure

Two independent stacks. Deploy either on its own.

| Stack | What it is | Deploy with |
| --- | --- | --- |
| `Multica-AgentRuntime` | One machine running the Multica daemon and the Claude Code runtimes agents execute on | `./scripts/deploy.sh` |
| `Multica-SelfHost` | Multica itself — web, API and PostgreSQL — on a fixed public IP, with every row of data in this AWS account | `./scripts/selfhost-deploy.sh` |

Nothing in CloudFormation ties them together. Run the agent runtime against
hosted multica.ai, or point it at your own server with
`-c serverUrl=https://<your-host>`.

---

# Agent runtime

One EC2 machine that runs the Multica daemon and the Claude Code runtimes our
agents execute on.

## What gets created

| Resource | Why |
| --- | --- |
| VPC, one AZ, `10.20.0.0/24` | Somewhere to put the instance. One AZ: the box holds no state a replacement would not re-create. |
| Security group, no ingress rules | The machine listens on nothing. The daemon dials out to Multica; Session Manager dials out to SSM. |
| EC2 instance, `t4g.medium` (Graviton, arm64) | Runs the daemon as the non-root `multica` user. |
| IAM role | Session Manager, Bedrock inference on Anthropic models, read on this machine's own SSM parameters, write to its own log group. Nothing else. |
| CloudWatch log group, 14-day retention | Daemon log and cloud-init output, shipped by the CloudWatch agent. |

There is no key pair and no SSH. Operators connect with Session Manager, which
also gives a single place to audit who went in.

Roughly **$30/month** in `ap-south-1`: `t4g.medium` ~$24, 40 GiB gp3 ~$3,
public IPv4 ~$4, logs under $1. Bedrock usage is billed per token on top, and
only when an agent falls back to the `claude-bedrock` runtime.

## The two runtimes

The machine installs Claude Code once and exposes it through two wrappers on
`PATH`. Each is the `command` of a [custom runtime
profile](https://multica.ai/docs/daemon-runtimes#custom-runtime-profiles) in
the workspace, so agents pick between them the same way they pick any runtime.

| Wrapper | Authentication | Role |
| --- | --- | --- |
| `claude-max` | `CLAUDE_CODE_OAUTH_TOKEN` from the team's Claude subscription | primary |
| `claude-bedrock` | `CLAUDE_CODE_USE_BEDROCK=1` plus the instance role | fallback |

Each wrapper unsets the other's credentials, so which one a run used is never
ambiguous. Adding a third provider, model or region later is one more wrapper
and one more profile — no change to this stack.

Multica has no automatic fallback between runtimes today: when the subscription
hits its limit the run fails with `provider_quota_limit` and someone switches
the agent to `claude-bedrock` and retries. The runtime fallback chain that
would automate this is application work, tracked separately.

## Credentials

Three values live in SSM Parameter Store as `SecureString` parameters under
`/multica/agent-runtime`, created by `scripts/bootstrap.sh` and never by
CloudFormation — no secret enters a template, a change set or the CDK context.

| Parameter | What it is |
| --- | --- |
| `multica-token` | Multica personal access token (`mul_…`). The daemon logs in with it, so its owner owns the registered runtimes and is the one who can make them public. |
| `claude-oauth-token` | Output of `claude setup-token` on a machine signed in to the subscription. |
| `github-token` | Fine-grained PAT or GitHub App installation token, limited to the agents' repositories. |

On the instance nothing is written to the root volume:

- `multica-fetch-secrets` pulls all three into `/run/multica` (tmpfs) on every
  service start, mode `0400`, owned by the `multica` user.
- The Multica PAT reaches `multica login` on stdin, not as an argument, so it
  stays out of `/proc/*/cmdline`.
- Git reads the GitHub token through a credential helper that reads tmpfs, so
  there is no `~/.git-credentials`.

Rotating a credential is `aws ssm put-parameter --overwrite` followed by
`sudo systemctl restart multica-daemon`.

## Deploying

Requirements: Node.js 22+, the AWS CLI, credentials for the target account, and
the [Session Manager
plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
for connecting afterwards.

```bash
pnpm install                      # from the repository root
cd infrastructure
./scripts/bootstrap.sh            # CDK bootstrap + the three SSM parameters
./scripts/deploy.sh               # shows a diff, asks, then deploys
./scripts/connect.sh              # Session Manager shell on the machine
```

`deploy.sh` leaves `--require-approval` at CDK's default, so a change that
widens a security group or an IAM policy stops and asks before it is applied.

First boot takes a few minutes. Watch it with `sudo cloud-init status --long`
on the machine, or read the `bootstrap` log stream in CloudWatch.

### Context overrides

```bash
./scripts/deploy.sh -c instanceType=t4g.large -c natGateway=true
```

| Context key | Default | Notes |
| --- | --- | --- |
| `instanceType` | `t4g.medium` | Graviton families only; the AMI is arm64 and synth rejects an x86 type. |
| `volumeSizeGb` | `40` | Checkouts plus `node_modules` for every concurrent run. |
| `natGateway` | `false` | See below. |
| `serverUrl` | `https://api.multica.ai` | Point at a self-hosted server instead. |
| `parameterPrefix` | `/multica/agent-runtime` | Must match what `bootstrap.sh` wrote. |
| `cliVersion` | `latest` | Pin a release tag (`v0.6.1`) for a reproducible build. |
| `nodeMajorVersion` | `22` | Node.js major Claude Code runs on. |
| `logRetentionDays` | `14` | Must be a CloudWatch retention period. |
| `maxConcurrentTasks` | `4` | `MULTICA_DAEMON_MAX_CONCURRENT_TASKS`. Parallel runs share this machine's CPU, disk and the same subscription quota. |
| `daemonId` | empty | `MULTICA_DAEMON_ID`. Multica keys runtimes by it, so pinning it lets a replacement instance take over the same runtimes, with their visibility and agent bindings. Empty lets the daemon mint its own on first boot. |

### Public subnet or NAT gateway

By default the instance sits in a public subnet with a public IP, no inbound
rules, and no key pair. It reaches npm, GitHub, the Anthropic API and Multica
directly. A NAT gateway would add about $33/month plus data processing for a
box that already accepts no inbound traffic.

`-c natGateway=true` moves it to a private subnet behind a NAT gateway if a
policy requires no instance to carry a public IP. Session Manager keeps working
either way, since the agent dials out.

## Workspace setup after the first deploy

The stack provisions the machine; the runtimes, agents and skills are workspace
configuration. Once the daemon is online (Multica → **Runtimes** shows
`multica-agent-runtime`):

1. Create the two custom runtime profiles, from any machine signed in to the
   workspace:

   ```bash
   multica runtime profile create --runtime-type claude \
     --command-name claude-max --display-name "Claude (subscription)"
   multica runtime profile create --runtime-type claude \
     --command-name claude-bedrock --display-name "Claude (Bedrock)"
   ```

   Only the machines that resolve the command on `PATH` register the runtime,
   so these appear on this box and nowhere else.

2. Mark both runtimes public, so every member's agents can use them. Only the
   runtime's owner — the identity behind `multica-token` — can do that.

3. Create the shared agents, bind the repositories to projects, and attach the
   team's conventions as skills.

## Operating it

```bash
./scripts/connect.sh                            # Session Manager shell
sudo systemctl status multica-daemon
sudo tail -f /opt/multica/.multica/daemon.log   # daemon log; CloudWatch stream <id>/daemon
sudo tail -f /var/log/multica/daemon.log        # unit output (secrets, login); stream <id>/service
sudo systemctl restart multica-daemon           # re-reads every credential
```

Changing `assets/bootstrap.sh` replaces the instance on the next deploy
(`userDataCausesReplacement`), which is the point: that script is the machine's
entire configuration, and an edit that only updated the launch template would
leave the running box behind.

## Accepted risks

- Agent runs skip approval prompts, so anyone who can assign an issue to a
  shared agent can run commands on this machine. That is acceptable for a
  trusted team on a box that holds no production credentials — the instance
  role reaches Bedrock and its own parameters, nothing else.
- All shared agents draw on one Claude subscription, so expect to fall back to
  Bedrock regularly.
- One instance, one AZ. If it is replaced, in-flight runs are lost; the issues
  stay in Multica and can be reassigned.

---

# Self-hosted Multica

Multica running on your own infrastructure: the Go backend, the Next.js web
app and PostgreSQL, from this repository's own `docker-compose.selfhost.yml`,
behind Caddy on a fixed public IP. Every issue, comment, agent and run lives in
the PostgreSQL volume this stack creates, in this AWS account. Nothing is sent
to multica.ai — the backend's first-party deployment telemetry is off unless
`-c telemetry=true` turns it back on.

## What gets created

| Resource | Why |
| --- | --- |
| VPC, one AZ, `10.30.0.0/24` | Somewhere to put the instance. |
| Elastic IP | A fixed address the team can bookmark, and the hostname the certificate is issued for. |
| Security group | 443 for the app, 80 for the ACME challenge and the HTTPS redirect. Nothing else — PostgreSQL is never published, and the app's own ports stay on loopback inside the instance. |
| EC2 instance, `t4g.medium` | Runs Compose and Caddy. |
| EBS data volume, encrypted | PostgreSQL's data directory and uploaded attachments. Separate from the instance and **retained when the stack is deleted**. |
| Daily snapshots | Data Lifecycle Manager, 7 kept by default. |
| CloudWatch log group | Bootstrap output. |

About **$33/month** in `ap-south-1`: `t4g.medium` ~$24, 30 GiB root + 20 GiB
data ~$5, Elastic IP ~$4, snapshots and logs under $1.

## HTTPS without owning a domain

A bare IP cannot have a trusted certificate, so by default the server answers
on `https://<your-ip>.sslip.io`. sslip.io is public wildcard DNS that resolves
any `1.2.3.4.sslip.io` to `1.2.3.4`, which gives Caddy a real hostname to get a
Let's Encrypt certificate for. You still reach the box at its own public IP;
only the name in the URL comes from elsewhere.

To use your own name instead, point an A record at the Elastic IP and redeploy
with `-c domainName=multica.example.com`.

## Deploying

```bash
cd infrastructure
./scripts/selfhost-setup.sh                                   # CDK bootstrap + generated secrets
./scripts/selfhost-deploy.sh you@example.com,teammate@example.com
```

The allowlist argument is required. Port 443 is open to the internet, so
without `ALLOWED_EMAILS` anyone who found the URL could sign up; synth refuses
to build the stack without it.

First boot pulls three images and completes an ACME challenge, so give it a few
minutes. The stack's `Url` output is where to go.

### Logging in

Multica authenticates with emailed verification codes.

- With a Resend API key stored by `selfhost-setup.sh`, codes are emailed.
- Without one, the backend prints each code to its own log:
  `./scripts/selfhost-apply.sh --code`.

The guide's `MULTICA_DEV_VERIFICATION_CODE` shortcut is deliberately never set
here — a fixed code on an internet-facing host means anyone who knows an email
address can log in as its owner.

## Operating it

```bash
./scripts/selfhost-apply.sh --status      # containers and proxy
./scripts/selfhost-apply.sh --logs        # bootstrap log
./scripts/selfhost-apply.sh --code        # latest login code
./scripts/selfhost-apply.sh               # re-run the bootstrap in place
MULTICA_STACK=Multica-SelfHost ./scripts/connect.sh   # a shell on the box
```

Unlike the agent runtime, this stack does **not** replace the instance when the
bootstrap script changes: the database volume is attached to it, and replacing
an instance out from under an attached volume is how people lose databases.
`cdk deploy` updates the launch template, and `selfhost-apply.sh` runs the new
script on the live machine.

### Pointing agents at it

```bash
./scripts/deploy.sh -c serverUrl=https://<your-host>
```

The daemon then registers its runtimes against your server instead of
multica.ai, and the PAT in `/multica/agent-runtime/multica-token` has to come
from your server's settings page.

### Context overrides

| Context key | Default | Notes |
| --- | --- | --- |
| `allowedEmails` | — | **Required.** Comma-separated addresses that may sign up. |
| `domainName` | sslip.io on the Elastic IP | Your own hostname; point an A record at the Elastic IP first. |
| `selfHostInstanceType` | `t4g.medium` | Graviton only. |
| `dataVolumeSizeGb` | `20` | PostgreSQL plus attachments. |
| `rootVolumeSizeGb` | `30` | OS and the three container images. |
| `snapshotRetentionDays` | `7` | Daily snapshots kept. |
| `imageTag` | `latest` | GHCR tag; pin a release for a reproducible deploy. |
| `telemetry` | `false` | `true` re-enables the first-party deployment telemetry. |

## If you delete the stack

The data volume is retained, so `cdk destroy` leaves the database behind along
with its snapshots and the SSM parameters. Attach the volume to a new instance,
or snapshot and delete it by hand once you are sure.

## Layout

```
infrastructure/
├── bin/app.ts                        CDK app: both stacks
├── lib/config.ts                     Context parsing and validation
├── lib/agent-runtime-stack.ts        Agent runtime: VPC, SG, role, instance, logs
├── lib/self-host-stack.ts            Self-host: EIP, SG, instance, data volume, snapshots
├── lib/bootstrap.ts                  Renders a script from assets/
├── lib/retention.ts                  CloudWatch retention mapping
├── assets/bootstrap.sh               Agent runtime first boot
├── assets/selfhost-bootstrap.sh      Self-host first boot
├── test/                             Assertions on the synthesized templates
└── scripts/
    ├── bootstrap.sh deploy.sh connect.sh destroy.sh        agent runtime
    └── selfhost-setup.sh selfhost-deploy.sh selfhost-apply.sh
```

`pnpm --filter @multica/infrastructure test` runs the template assertions —
no AWS credentials, no deploy. They cover the claims that are easy to get wrong
by hand.

For the agent runtime: no inbound rules, no key pair, one security group, an
instance role that reaches only Bedrock, Parameter Store and its own log group,
an encrypted root volume, IMDSv2, and a bootstrap that starts the daemon as a
non-root user without writing a credential to disk.

For the self-host: only 80 and 443 open and never 5432, 3000, 8080 or 22; a
database volume that is encrypted, separate from the instance and retained on
delete; daily snapshots; telemetry off; the signup allowlist reaching the
backend; the WebSocket route ahead of the catch-all; and no fixed development
verification code.

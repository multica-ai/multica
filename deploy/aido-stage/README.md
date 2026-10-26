# AIDO stage 1 runtime configuration candidate

This is a disabled template, not an installed launchd service. No production
daemon or relay binding has been changed. The account `_aido1565` (UID/GID 490)
was created after issue impact comment 9522a304 on 2026-10-26. It has a private
staging home and `/usr/bin/false` login shell, and is not in admin/staff.

The agent configuration must use a new dedicated AIDO agent and runtime, never
reuse the TeamLeader agent. No platform agent/runtime has been created yet.
Bind only the new runtime; provide no project repo/local_directory resources
pointing to the main user's home, and no shared history/resume/workdir.
Do not give the agent a broad PAT or copy provider credentials from the main
account. A runtime credential is held by its daemon; task credentials must be
reviewed for the minimal AIDO API scope before enabling any real owner task.
The current default task-token permissions are not claimed to satisfy that gate.

`env -i` avoids inherited secrets. HOME, binaries, logs and workdir are specific
to the new user. The model's own fresh login/configuration must live there;
subscription/authentication compatibility has not been tested. The current
staging tests use mock model output and never read provider/Google token bytes.

The plist is deliberately RunAtLoad=false and KeepAlive=false and was only
linted. Do not install/bootstrap it until the complete relay lane integration,
task token scope, independent validation and cutover decision are available.

Before a future install, record the binary SHA, credential provisioning method,
new runtime/agent UUIDs, final plist path/label, permissions and affected PIDs in
the issue. Installation would copy a root-owned 0644 plist to
`/Library/LaunchDaemons/co.nexai.aido1565.stage.plist` and bootstrap that label
only. Those operations are not performed by this PR or staging scripts.

Rollback stops only that new label (`launchctl bootout system/co.nexai.aido1565.stage`),
parks AIDO ingress, revokes the new task/runtime credentials and collects the new
user's process list. Never restart the old broad lane as a security fallback.
After new-user process count is zero, remove only the new user/group records and
their staging artifacts if explicitly retiring them. Preserve evidence first.

Production relay PID 59565 and the existing daemon are outside this plan's
staging changes. Home 0750 and aido state 0700 already deny the new non-staff user
traversal; no chmod/chown of production paths was needed for the staging proof.

Phase 2 egress enforcement is deferred by router decision. Arbitrary public web
fetch and localhost service access remain risks in stage 1; do not describe this
as a full network sandbox. Calendar invitation titles are no longer included in
the relay's owner note; only parsed bounded start/end timestamps are included.

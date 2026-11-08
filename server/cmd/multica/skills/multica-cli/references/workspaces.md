# Cold setup and warm workspace reuse

Repeated code work can reuse a prepared project directory instead of rebuilding
its environment for each task. A **cold setup** prepares a missing environment;
a **warm start** reuses an existing one after checking it is still appropriate.
Neither means the model remembers a previous conversation. An empty persistent
directory is still cold, and attaching a resource alone does not install
dependencies, transfer files, or guarantee faster execution.

## Check before handing off work

Before creating an agent-assigned active issue, assigning fresh work, or
starting a run:

```bash
multica project list --output json
multica project resource list <project-id> --output json
multica agent get <agent-id> --output json
multica runtime list --output json
```

1. Identify the intended project's resource, absolute path, execution mode and
   owning `daemon_id`.
2. Follow the assignee's actual runtime binding and compare its daemon with the
   resource's owner. For squads or dynamic routing, resolve the executor or
   explicitly report it as unknown; the project lead is not proof of execution.
3. Resolve daemon IDs to device names and check online status and last-seen
   freshness. Report `device · path · mode · online`, not a bare UUID.
4. Verify directory access and the required checkout/dependencies on that
   machine using an available authorized read-only mechanism. The computer
   sending this command may be different from the executor. `multica daemon
   status` only checks the local daemon; path spelling and hostname resemblance
   do not establish ownership.
5. With several resources, establish which one will be selected. Do not assume
   list order or that adding a resource rebinds the assignee's runtime.

If these checks match the user's authorized scope, reuse the binding without
asking again. If execution ownership is ambiguous, the machine is offline, or
remote directory access cannot be verified, report the missing fact and pause
the execution-dependent action. Continue independent reads or requested
backlog-only bookkeeping. Do not silently rebind an agent or fall back to a
temporary directory. Existing issue updates with `--no-start` do not require
repeating execution setup.

## Cold setup: choose and prepare once

When no suitable binding exists, first look for the user's existing checkout
on the intended executor. Propose the device, absolute path, and mode; confirm
unless that concrete choice is already authorized. Honor project/workspace
directory conventions. Never embed personal device names, paths, or secrets in
a shared skill.

- `local_directory` + `in_place`: reuse that directory and its prepared files
  and dependencies. Multica serializes its runs there; human editors and other
  tools are not locked, so coordinate concurrent changes.
- `local_directory` + `worktree`: isolate runs in Git worktrees. Verify the
  repository has a commit and the target daemon supports worktree execution.
  Check how dependencies and local files are prepared; isolation does not mean
  every ignored file or installed dependency is copied into the new worktree.
- `github_repo`: retain remote repository context. It does not by itself
  synchronize or prepare a local-directory resource on another computer.
- Temporary execution: appropriate when the user chooses a disposable task
  whose files and environment need not persist.

After authorized preparation, attach the resource and read it back:

```bash
multica project resource add <project-id> --type local_directory --local-path <absolute-path> --daemon-id <owning-daemon-id> --execution-mode in_place --output json
multica project resource list <project-id> --output json
multica issue create --title "<title>" --project <project-id> --status backlog --output json
```

Use a status supported by the project's workflow; the example parks an issue
in the default workflow. Apply the execution checks before assigning or
activating it. Read back configuration separately from checking a real run's
executing device and working directory.

## Warm start: reuse, then verify the actual run

Re-read live bindings on each fresh handoff. Reuse the established environment
when device, directory, mode, readiness and authorized scope still match. Do
not recreate projects, reinstall everything, or ask the same preference question
on every issue. Inspect what changed and prepare only what the task needs.

An authorized real run's execution evidence must identify the expected machine
and working directory before reporting execution verified. Do not promise warm
starts based on a resource entry alone.

## Switching devices

Changing the computer used to send instructions does not require changing the
executor. If execution itself must move, authorize that change, inspect both
working trees, and agree on the revision/files to transfer. Uncommitted edits,
ignored files, local data, dependencies and credentials need separate handling.
Verify the destination before replacing the old binding; do not promise
automatic cross-device synchronization.

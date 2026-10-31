# Chat working directory

Approved scope: each new Web/Desktop chat can browse or enter an existing directory on its agent runtime's daemon host. The session retains that machine and path for every subsequent turn. Selecting another directory starts a new conversation. Existing chats and project-resource behavior retain their defaults.

## Architecture

The daemon owns filesystem discovery, path validation and execution. Multica's existing pending-action heartbeat transport supplies directory listings, with shared Redis request storage for multi-node deployments. A nullable chat_session.working_directory JSON object stores {daemon_id, path}; an explicit selection takes precedence over the project's local_directory execution location while preserving project context. Existing LocalWorkDir preparation and ExecOptions.Cwd adapters execute the chat in place, including their user-directory cleanup protections and conversation session stores.

## Approaches

A Codex-only filesystem browser would exclude other providers; per-provider browsers would duplicate operating-system behavior. Use one daemon filesystem browser and existing provider Cwd interfaces. Binding a project directory remains supported but cannot express the requested per-conversation choice.

## Boundaries

Browse and create a custom-directory chat only for an owned, online local runtime advertising chat-working-directory-v1, with a daemon id. Cloud execution and OpenClaw gateway mode do not expose a local-host directory selector. Server and daemon both reject a mismatched machine or unsupported capability rather than silently execute in a different location. Browsing may traverse home/root; selection uses existing local-directory validation. The first version uses existing directories in place; no folder creation, SSH service, worktree picker, mobile UI, or global workspace-root setting is added.

## Verification

Test session persistence and claim propagation, runtime/machine/capability rejection, remote directory and symlink handling, continuation on the same directory, malformed API parsing, and selection/send wiring on both shared chat surfaces. Default tests use fake agent binaries, never installed agent CLIs.

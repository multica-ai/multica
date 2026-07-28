# Local release capacity loop

This directory contains the macOS guardrails for heavy local release work.
They complement the daemon claim admission gate: the daemon gate protects new
claims, while these entrypoints protect heavy work started by an already-running
agent task.

## Entrypoint matrix

| Operation | Required host headroom | Entrypoint | Terminal cleanup owner |
| --- | ---: | --- | --- |
| amd64 production image build/push | 22 GiB | `multica-heavy-batch --operation release-amd64` | wrapper: declared temp roots/images, builder cache, guest `fstrim`; deletes a profile created for this batch only when it has zero running containers |
| amd64 local preview build | 22 GiB | `multica-heavy-batch --operation preview-amd64` | wrapper, same rules |
| other Docker/BuildKit build | 22 GiB | `multica-heavy-batch --operation docker-build` | wrapper, same rules |
| local Next.js build without VM | 12 GiB | `multica-heavy-batch --operation next-build` | wrapper: declared batch temp roots |
| Multica CLI/desktop release | 16 GiB | `multica-heavy-batch --operation multica-release` | wrapper: declared batch temp roots |
| local preview container | budget belongs to its build operation | `multica-preview-run` | `multica-preview-cleanup`: only labelled, expired, non-running containers |
| terminal agent run workdir | threshold GC at host free space below 22 GiB; 24h TTL | `multica-workspace-cleanup` | cleanup script after process, terminal metadata, Git dirty, upstream, and ahead checks |

`--required-gib` may raise an operation budget. It cannot lower the floor.
Insufficient headroom exits `75` with `result=PARKED` before command, VM, build,
manifest, or temp-root side effects.

## Colima policy

- `default` is reserved and is never accepted by the wrapper.
- New amd64 builds use an ephemeral owner-bound profile:
  `KAP-1184` must use `kap1184-amd64`. The global heavy-batch lock permits one
  such profile at a time.
- The wrapper rejects mismatched profile names. A newly created profile is
  deleted after success or failure only when it has zero running containers.
- A pre-existing profile is never deleted by the wrapper.
- The wrapper never stops a profile that was already running when the batch
  began.
- A profile with running containers is preserved.
- Images are removed only when passed as exact `--cleanup-image` values.
- Builder cache is regenerable and is pruned only after the wrapped command has
  terminated and no other local Docker build process is visible.
- `fstrim` runs after Docker cleanup; guest and host free-space observations are
  printed. A profile containing a configured protected production/rollback
  baseline is retained.

## Examples

```bash
build_root="$(mktemp -d /tmp/kap-1184-release.XXXXXX)"

multica-heavy-batch \
  --operation release-amd64 \
  --owner KAP-1184 \
  --profile kap1184-amd64 \
  --temp-root "$build_root" \
  -- \
  docker --host "unix://${HOME}/.colima/kap1184-amd64/docker.sock" \
    buildx build --platform linux/amd64 --push "$build_root/repo"
```

```bash
multica-preview-run \
  --profile kap1184-amd64 \
  --owner KAP-1184 \
  --ttl-hours 8 \
  -- --detach --name kap1184-preview IMAGE
```

## Workspace GC safety

A whole task root is eligible only when all conditions hold:

1. `.gc_meta.json` has a supported kind and `completed_at`.
2. The completion age exceeds the configured TTL.
3. A global open-file scan does not map any live path into the task root.
4. Every nested repository has an upstream, `ahead=0`, and no dirty files
   outside the explicit regenerable directory names.
5. A second recursive open-file scan passes immediately before deletion.
6. The resolved path matches exactly
   `<approved-root>/<workspace-uuid>/<8-hex-task-id>`.

Any missing or failed signal protects the task root. The cleanup never touches
credentials, profiles, volumes, project directories, or arbitrary cache roots.

Run dry first:

```bash
MULTICA_CLEANUP_DRY_RUN=1 multica-workspace-cleanup --dry-run
```

## Verification

```bash
scripts/macos/test-capacity-loop.sh
```

The fixture verifies:

- insufficient headroom creates no command side effect and exits `75`;
- clean terminal workdirs are eligible;
- dirty, ahead, active, and young workdirs survive;
- success and controlled failure remove only declared batch temp roots;
- wrapper cleanup invokes `fstrim` and stops only an idle profile it started;
- expired terminal previews are removed while a running preview survives.

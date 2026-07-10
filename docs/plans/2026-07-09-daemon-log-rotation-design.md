# Daemon Log Rotation Design

## Context

The default Multica daemon log reached 188 MiB and two named-profile logs reached 94 MiB and 9.6 MiB. Background daemons inherit stdout and stderr as append-only file descriptors, so renaming the active file alone would not reclaim space: the daemon would continue writing to the renamed inode.

## Options considered

1. Rotate only at daemon startup. This is simple and lossless, but a long-running daemon can still grow without bound between restarts.
2. Replace every daemon logger and stderr writer with a third-party rolling writer. This gives precise rotation but is invasive and does not automatically cover panic or direct stderr output.
3. Periodically copy the active log to numbered backups and truncate the original inode. This preserves the file descriptor used by stdout and stderr, works across default and named profiles, and bounds growth without changing daemon logging call sites.

Option 3 is selected. It is the smallest change that addresses the actual append-only descriptor behavior.

## Design

- The background parent passes the resolved log path to the foreground daemon through `MULTICA_DAEMON_LOG_PATH`.
- The foreground daemon performs one rotation check before starting work, then checks periodically while running.
- When the active log exceeds 25 MiB, the daemon copies only its most recent 25 MiB to `daemon.log.1`, shifts the previous archive to `daemon.log.2`, and truncates the active file in place. Bounding the temporary copy and every numbered archive avoids needing free space proportional to an already-oversized source log.
- Rotation failures are warnings and never prevent the daemon from running.
- Manual foreground runs do not rotate because they have no background log path environment variable.

## Correctness and failure handling

- The archive copy is written to a temporary file and synced before backup names change.
- The active log is truncated only after the archive has been installed, so a pre-truncate failure preserves the source log.
- The rotator truncates the source through its opened descriptor rather than resolving the pathname again. This keeps inherited append-only file descriptors on the same active inode even if the path changes concurrently.
- Copy-truncate has an accepted small race window: bytes appended after the source size snapshot are outside the bounded archive and may be removed by truncation. Daemon logs are best-effort operational output, and preserving the inherited inode without introducing a process-wide logging rewrite is the chosen tradeoff.
- Tests cover below-threshold no-op behavior, bounded-tail archive shifting, and appending through a descriptor held open across in-place truncation.

## Operational companion

The CPA capture job is a separate growth source. Its existing disk watermark disables raw-log storage only below 10 GiB, while the system alert threshold is 15 GiB. The cutoff will be aligned to 15 GiB with a regression test so raw capture stops before the host reaches the critical alert boundary.

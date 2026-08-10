# Fork maintenance

This repository is the actively maintained Multica fork for `shizukanaskytree`.

## Remotes

```text
origin    https://github.com/shizukanaskytree/multica.git
upstream  https://github.com/multica-ai/multica.git
```

`upstream` remains the source for official Multica updates. `origin` contains the maintained product, including Agent Cockpit and future Web PTY work.

## Maintenance policy

- Review upstream changes regularly, but do not merge them mechanically.
- Keep relevant security fixes, migrations, compatibility work, and infrastructure improvements.
- Evaluate upstream product and UI changes against the maintained Agent Cockpit direction.
- Preserve local features when resolving conflicts; document intentional divergence.
- Keep the official license, copyright, and source attribution.
- Record the upstream commit used by each maintained release.

## Branches

- `main`: stable maintained product.
- `feat/*`: independently developed features.
- `fix/*`: maintained-product fixes.
- `sync/upstream-YYYYMMDD`: reviewed upstream synchronization.

## Syncing upstream

```bash
git fetch upstream --prune
git switch main
git pull --ff-only origin main
git switch -c sync/upstream-YYYYMMDD
git merge upstream/main
```

Resolve conflicts without overwriting maintained features. Run the Core, Views, Server, migration, and self-host build checks, then merge the synchronization through a reviewed pull request.

Do not force-push published `main`, deploy unreviewed upstream changes, or treat synthetic task data as product acceptance evidence.

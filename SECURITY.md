# Security Policy

## Supply Chain Attack Protection

**Why this matters:** In May 2026, the TanStack library (used in millions of web apps) was compromised. Attackers published malicious versions that stole AWS, GCP, and Vault credentials from any developer who ran `npm install` during a 10-minute window. NPM could not unpublish the malicious versions due to third-party dependencies.

This is not an isolated incident. The same attack pattern has hit:
- `tj-actions/changed-files` GitHub Action — exposed CI secrets across thousands of repos
- `chalk`, `debug`, `ansi-styles` npm packages via maintainer compromise
- VSCode marketplace extensions acting as credential stealers

The frequency is increasing because the payoff is massive: one compromised package reaches millions of machines in hours.

## What We Do Here

### Pinned npm versions (`save-exact=true` in `.npmrc`)

All dependencies are installed with exact versions — no `^` or `~` ranges. This means `npm install` installs exactly what is in the lockfile, not a newer compatible version that could be malicious.

**Lockfile (`pnpm-lock.yaml`) is committed to git.** This freezes the entire resolved dependency tree. Any change to the lockfile is visible in code review.

### Pinned GitHub Actions (SHA in `.github/workflows/`)

All GitHub Actions are pinned to a specific commit SHA instead of a version tag like `@v4`. Example:
```yaml
# BEFORE (unsafe — tag can be moved to point to malicious commit)
uses: actions/checkout@v4

# AFTER (safe — SHA is immutable)
uses: actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5 # v4
```

A compromised action maintainer can push malicious code and move the version tag to it. A SHA pin is immutable — the code you reviewed is the code that runs.

### Dependabot Alerts (enabled)

GitHub Dependabot monitors all dependencies for known vulnerabilities and creates automated PRs when fixes are available.

## What to Do When Updating a Dependency

1. Update the version in `package.json`
2. Run `pnpm install` to regenerate the lockfile
3. Review the lockfile diff before committing — check for unexpected new packages
4. Search the lockfile for `postinstall`, `preinstall`, or `prepare` scripts in new packages

## Emergency: If You Installed During a Suspected Attack Window

1. Rotate all cloud credentials: AWS, GCP, Kubernetes service accounts
2. Rotate GitHub tokens and SSH keys
3. Revoke active sessions on GitHub, npm, cloud providers
4. Check audit logs for unauthorized API calls
5. Wipe local bash/zsh history
6. If the install ran as root: rebuild the machine from scratch

## Reference

- [TanStack incident (May 11, 2026)](https://github.com/TanStack/router/issues/7383)
- [tj-actions/changed-files compromise](https://github.com/tj-actions/changed-files/issues)
- OWASP Top 10: A08 Software and Data Integrity Failures

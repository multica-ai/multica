Purpose: Verify that the CLI managed update flow works — the CLI checks a manifest for new versions, downloads updates from the configured source, and applies them with backup/restore safety.

Preconditions: The Multica CLI is installed locally. The manifest URL is configured (pointing to the OSS bucket or configured manifest endpoint). A newer version is available in the manifest than the currently installed version.

User flow: Run `multica --version` to note current version. Run the CLI update command or trigger the auto-update check. The CLI should detect a newer version from the manifest, download the update binary, back up the current binary, and replace it with the new version. Run `multica --version` again to confirm the update.

Expected results: The CLI compares its current version against the manifest-reported latest version. When a newer version is available, it downloads the update. The existing binary is backed up before replacement. After update, `multica --version` shows the new version number. If the update fails mid-way, the backup is restored (the CLI remains functional). The version comparison correctly handles semantic versioning and described git tags.

Notes for automation: This test modifies the CLI binary. Run in an isolated environment or verify rollback works. The manifest URL can be checked with `curl` independently. Version comparison logic should handle pre-release tags correctly.

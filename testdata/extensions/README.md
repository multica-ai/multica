# Mock Platform Extensions

`runtime-pool-demo.zip` is the client-importable Platform Extension package for manually checking the Runtime Pool flow. Its root `codeagent-extension.json` owns the Extension name, version, and description. The package keeps Agents in `agents/`, Commands in `commands/`, and Skills with their supporting files in `skills/`.

## Client steps

1. Open the target workspace.
2. Open **扩展** in the workspace sidebar.
3. Click **导入扩展**.
4. Select `testdata/extensions/runtime-pool-demo.zip`.
5. Confirm that the release shows three agents, two Skills, one Pool Coordinator leader, and the imported Runtime mapping.
6. Create an Issue assigned to **Pool Coordinator** and verify that it first enters `waiting_runtime`, then is assigned by an eligible Pool Runtime.

Uploading the same file again checks idempotency: the existing release and native resources should be reused instead of creating duplicates.

`runtime-pool-demo/` is the editable package source. Run `node scripts/create-extension-zip.mjs` after changing it; the builder recursively packages that directory and ignores macOS `.DS_Store` files.

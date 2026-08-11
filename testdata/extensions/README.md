# Mock Platform Extensions

`runtime-pool-demo.source.json` is a client-importable Platform Extension source document for manually checking the Runtime Pool flow.

## Client steps

1. Open the target workspace.
2. Open **扩展** in the workspace sidebar.
3. Click **导入扩展**.
4. Select `testdata/extensions/runtime-pool-demo.source.json`.
5. Confirm that the release shows three agents, two Skills, one Pool Coordinator leader, and the imported Runtime mapping.
6. Create an Issue assigned to **Pool Coordinator** and verify that it first enters `waiting_runtime`, then is assigned by an eligible Pool Runtime.

Uploading the same file again checks idempotency: the existing release and native resources should be reused instead of creating duplicates.

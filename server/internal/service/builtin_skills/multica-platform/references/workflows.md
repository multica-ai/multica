# Agent workflows

The workflow builder creates a directed graph of `start`, `agent`, `human_task`,
`human_review`, `condition`, `parallel`, `merge`, and `end` nodes. A workflow
run snapshots the selected draft or immutable release graph. Editing the draft
later changes future runs only; it never changes an active run.

An agent node is a normal agent task with `origin_type=workflow`. Its prompt
contains the run input, the final text output from direct upstream nodes, and
any explicitly referenced ancestor outputs. Local filesystem paths are not
shared between nodes, so publish useful artifacts through the platform's
durable links or include the result in the final response.

When running a workflow node:

- Execute only the node instructions and return the result in the final
  response. Do not dispatch, delegate, or advance another workflow node.
- Do not infer success from a manually changed issue status. The scheduler
  marks the node successful only after the associated execution finishes and
  stores its output.
- A failed node blocks its downstream nodes while independent branches may
  continue. A user retry creates a new execution attempt for the same node and
  retains the earlier attempt and successful sibling outputs.
- Human tasks and reviews are durable work items. Submit, approve, rework,
  transfer, and deadline extension commands are version-fenced; the current
  assignee must be a workspace member. Replaying a submitted decision with the
  same idempotency key is safe, while reusing that key for different values is
  rejected.
- A node may route a failure through an explicit failure/timeout edge or create
  a recovery work item for the workflow owner. Termination, takeover, and
  uncertain-result resolution are explicit human actions and never change the
  published graph.
- Return concise, machine-readable text when the node instruction requests it;
  include artifact URLs alongside the explanation when files were produced.

The builder Chat can change only the workflow draft. Running the workflow is a
separate explicit user action in the editor.

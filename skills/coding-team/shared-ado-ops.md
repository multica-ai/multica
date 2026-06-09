---
name: Shared ADO Operations
description: Azure DevOps CLI patterns shared across all coding-team agents — two separate ADO instances for work items vs. code
---

# ADO Operations Reference

Operations span **two separate ADO instances**. Always prefix `az` calls with the correct PAT inline so neither variable bleeds into the other:

| Instance | Org URL | Project | PAT env var | Used for |
|----------|---------|---------|-------------|----------|
| incyclesoftware | `https://dev.azure.com/incyclesoftware` | `ineight` | `ADO_PAT_INCYCLE` | Work items, boards, comments |
| code repo | `https://dev.azure.com/{code_org}` | `{code_project}` | `ADO_PAT_INEIGHT` | Git repos, pull requests |

Prefix pattern:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards ...   # work items
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INEIGHT  az repos ...   # PRs
```

**Do not use `az rest` for ADO endpoints.** It tries to acquire an Azure AD token and fails against `dev.azure.com` (you'll see `Can't derive appropriate Azure AD resource from --url` followed by an HTML sign-in page). Use `curl` with Basic auth and the PAT instead. The PAT goes in the password slot with an empty username:
```bash
curl -sS -u ":$ADO_PAT_INCYCLE" -H "Content-Type: application/json" "<uri>"
```

For git operations, embed the PAT in the URL — never print it:
```bash
REPO_URL="https://anything:$ADO_PAT_INEIGHT@dev.azure.com/${CODE_ORG}/${CODE_PROJECT}/_git/${REPO_NAME}"
```

`CODE_ORG`, `CODE_PROJECT`, and `REPO_NAME` come from the master issue state. Old issues may omit them; use `ineight`, `Platform`, and `AgenticAI` as backward-compatible defaults.

When the `ado_payload_normalize` deterministic tool is available, use it after
fetching ADO JSON to normalize supplied payloads. It does not call ADO. It only
converts already-fetched work items, comment responses, child-item batches, and
ancestor arrays into plain text fields the planning skills can consume:

```json
{
  "work_item": {},
  "comments_response": {},
  "child_items_response": {},
  "ancestors": []
}
```

Use its `machine_data.work_item.description`,
`machine_data.work_item.acceptance_criteria`, `machine_data.comments`,
`machine_data.active_child_tasks`, and `machine_data.nearest_component` instead
of repeating ad hoc HTML stripping and active-task filtering. If the tool is not
available, use the command snippets below.

---

## Fetch a work item

```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {id} \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

Key fields under `.fields`:
- Title: `System.Title`
- Description (HTML): `System.Description`
- Acceptance criteria (HTML): `Microsoft.VSTS.Common.AcceptanceCriteria`
- Area path: `System.AreaPath`
- Iteration path: `System.IterationPath`
- Work item type: `System.WorkItemType`
- State: `System.State`

Strip HTML tags with:
```bash
echo "$HTML_STRING" | python3 -c "import sys, re; print(re.sub(r'<[^>]+>', '', sys.stdin.read()).strip())"
```

Parse acceptance criteria into a bullet list by splitting on `</li>` boundaries after stripping tags.

---

## Fetch work item comments

```bash
curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  "https://dev.azure.com/incyclesoftware/ineight/_apis/wit/workItems/{id}/comments?api-version=7.1-preview.4"
```

The response has a `.value` array. Each entry contains:
- `.createdBy.displayName` — author
- `.createdDate` — ISO timestamp
- `.text` — HTML; strip tags for plain text

Store comments as an array ordered oldest → newest. If `.value` is absent or empty, store `[]`.

---

## Create a task work item

Use `--query id --output tsv` to capture only the new work item's id. **Do not pipe `--output json` into Python or `jq` here** — `az`'s JSON output occasionally contains backslashes (e.g. `System.AreaPath: "ineight\Team"`) that strict JSON parsers reject with `Invalid \escape`. Server-side projection avoids the parse step entirely.

```bash
ADO_ID=$(AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item create \
  --title "{ado_title}" \
  --type "Task" \
  --description "Child of #{deliverable_id}." \
  --area "{area_path}" \
  --iteration "{iteration_path}" \
  --org https://dev.azure.com/incyclesoftware \
  --project ineight \
  --query id \
  --output tsv)
```

`$ADO_ID` is now the integer work-item id. The `ado_title` must be ≤ 50 characters — a concise action phrase only. Never put detailed descriptions, acceptance criteria, or language tags in ADO.

**Idempotency:** verify `$ADO_ID` is non-empty before continuing. If empty (rare — `az` succeeded but returned no id), stop and surface the failure. Do not retry the create blindly — work-item creation is not idempotent and a blind retry will produce duplicates.

---

## Link a task as child of the deliverable

```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item relation add \
  --id {task_ado_id} \
  --relation-type "Parent" \
  --target-id {deliverable_id} \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

If this fails after the task was created successfully, log a warning and continue — the human can fix the link manually.

---

## Post a comment to a work item

Write the body to a temp file to avoid shell quoting issues:

```bash
python3 -c "
import json, sys
issues = sys.argv[1:]
items = ''.join(f'<li>{i}</li>' for i in issues)
payload = {'text': f'<ul>{items}</ul>'}
print(json.dumps(payload))
" "Issue one" "Issue two" > /tmp/ado_comment.json

curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  -X POST \
  --data-binary @/tmp/ado_comment.json \
  "https://dev.azure.com/incyclesoftware/ineight/_apis/wit/workItems/{ado_id}/comments?api-version=7.1-preview.4"
```

Format the `text` field as an HTML fragment (`<ul><li>...</li></ul>`) — ADO renders it as rich text.

---

## Fetch child work items of a deliverable

Fetch with relations expanded:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {deliverable_id} \
  --expand relations \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

Filter `.relations[]` where `.rel == "System.LinkTypes.Hierarchy-Forward"`. The child ID is the trailing path segment of `.url` (after `/workItems/`).

Batch-fetch child details:
```bash
cat > /tmp/ado_batch.json <<'EOF'
{"ids":[{comma-separated ids}],"fields":["System.Id","System.Title","System.Description","Microsoft.VSTS.Common.AcceptanceCriteria","System.State"]}
EOF

curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  -X POST \
  --data-binary @/tmp/ado_batch.json \
  "https://dev.azure.com/incyclesoftware/_apis/wit/workitemsbatch?api-version=7.1"
```

Skip any child whose `System.State` is `Done` or `Closed`.

---

## Fetch parent/ancestor work items

Use this when a task or deliverable needs broader ADO context, such as the owning **Component**. Do not assume a fixed hierarchy depth: the Component might be the direct parent of the deliverable, or it might be a parent of a parent.

Fetch one work item with relations expanded:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {work_item_id} \
  --expand relations \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

The parent link is the relation where `.rel == "System.LinkTypes.Hierarchy-Reverse"`. The parent ID is the trailing path segment of `.url` after `/workItems/`.

Walk upward from the deliverable ID until you find the nearest Component, or until there is no parent:
```bash
CURRENT_ID={deliverable_id}
ANCESTORS_JSON='[]'
ADO_COMPONENT_JSON='null'

for DEPTH in 1 2 3 4 5 6 7 8 9 10; do
  ITEM=$(AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
    --id "$CURRENT_ID" \
    --expand relations \
    --org https://dev.azure.com/incyclesoftware \
    --output json)

  PARENT_ID=$(python3 - "$ITEM" <<'EOF'
import json, sys
item = json.loads(sys.argv[1])
for rel in item.get('relations') or []:
    if rel.get('rel') == 'System.LinkTypes.Hierarchy-Reverse':
        print(rel.get('url', '').rstrip('/').split('/')[-1])
        break
EOF
)

  if [ -z "$PARENT_ID" ]; then
    break
  fi

  PARENT=$(AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
    --id "$PARENT_ID" \
    --expand relations \
    --org https://dev.azure.com/incyclesoftware \
    --output json)

  ANCESTORS_JSON=$(python3 - "$ANCESTORS_JSON" "$PARENT" "$DEPTH" <<'EOF'
import json, sys
ancestors = json.loads(sys.argv[1])
item = json.loads(sys.argv[2])
fields = item.get('fields') or {}
ancestors.append({
    'id': item.get('id'),
    'depth': int(sys.argv[3]),
    'type': fields.get('System.WorkItemType', ''),
    'title': fields.get('System.Title', ''),
    'area_path': fields.get('System.AreaPath', ''),
})
print(json.dumps(ancestors))
EOF
)

  IS_COMPONENT=$(python3 - "$PARENT" <<'EOF'
import json, sys
item = json.loads(sys.argv[1])
fields = item.get('fields') or {}
wit = (fields.get('System.WorkItemType') or '').strip().lower()
title = (fields.get('System.Title') or '').strip().lower()
print('yes' if wit == 'component' or title.startswith('component:') else 'no')
EOF
)

  if [ "$IS_COMPONENT" = "yes" ]; then
    ADO_COMPONENT_JSON=$(python3 - "$PARENT" "$DEPTH" <<'EOF'
import json, re, sys
item = json.loads(sys.argv[1])
fields = item.get('fields') or {}
component = {
    'id': item.get('id'),
    'depth': int(sys.argv[2]),
    'type': fields.get('System.WorkItemType', ''),
    'title': fields.get('System.Title', ''),
    'description': re.sub(r'<[^>]+>', '', fields.get('System.Description', '')).strip(),
    'area_path': fields.get('System.AreaPath', ''),
}
print(json.dumps(component))
EOF
)
    break
  fi

  CURRENT_ID="$PARENT_ID"
done
```

Use the nearest Component found. If no Component is found within 10 parent hops, continue with deliverable/task context and note that no ADO Component was found; do not fail planning solely because the hierarchy is missing or irregular.

---

## Hard rules

- Never run `az boards work-item update --state ...` on any work item. Board state is owned by the human.
- Never send the task's detailed `title`, `description`, or `acceptance_criteria` to ADO. Those fields live only in the Multica master issue state.
- Never mention AI, agents, or automation in any ADO work item title, description, or comment.

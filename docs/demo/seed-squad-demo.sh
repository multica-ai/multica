#!/usr/bin/env bash
#
# seed-squad-demo.sh - 一键导入「跨员工 Agent 协作」演示数据。
#
# 做三件事（幂等，可重复执行）：
#   1. 创建（或复用同名）squad「代码交付小队」，leader = 资深工程师；
#   2. 加入 4 个专项成员（架构 / 编码 / 测试 / 文档），leader 本身也作为成员登记；
#   3. 写入 squad 指令（leader 编排手册，会在 leader 接管任务时注入其 prompt）。
#
# 只创建数据，不触发任何 agent 运行（无 issue 被分派）。
# 导入完成后，按 docs/demo/squad-collaboration.md §6 创建并分派一个 issue 即可启动演示。
#
# 依赖：multica CLI（已 auth login）、python3（解析 JSON）。
# 兼容 macOS 自带 bash 3.x（不使用关联数组）。
set -euo pipefail

SQUAD_NAME="代码交付小队"
SQUAD_DESC="代码交付演示小队：leader 编排，架构/编码/测试/文档协作交付一个需求。见 docs/demo/squad-collaboration.md"

# name|member-type|role  --  第一个必须是 leader agent
MEMBERS=(
  "资深工程师|agent|leader"
  "量化架构师agent|agent|架构"
  "高级工程师|agent|编码"
  "测试agent|agent|测试"
  "paper agent|agent|文档"
)

read -r -d '' INSTRUCTIONS <<'INSTRUCTIONS_EOF' || true
# 代码交付小队 · 编排手册

本小队按以下流水线协作交付需求：

1. 架构（量化架构师agent）：先出技术方案 / 接口设计 / 影响面评估，以 issue 评论回传。
2. 编码（高级工程师）：拿到架构方案后实现，改动 / PR 以评论回传。
3. 测试（测试agent）：拿到实现后端到端验证，结论以评论回传。
4. 文档（paper agent）：交付后写变更说明 / changelog。

leader（资深工程师）职责：
- 读需求后按上面顺序逐个 @mention 派活，不要一次性全派。
- 每一轮派活后调 `multica squad activity <issue-id> action --reason "<简述>"` 记录评估。
- 成员回传后再次被触发时，评估是否进入下一阶段。
- 全部完成后推进 issue 状态并 @mention 提交者汇报。
- 需求不清时 @mention 提交者澄清，不要自行假设。
INSTRUCTIONS_EOF

# ---------- helpers ----------

die() { echo "ERROR: $*" >&2; exit 1; }

require_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing dependency: $1"; }
require_cmd multica
require_cmd python3

# 缓存 agent 列表，避免多次调用 CLI
AGENT_LIST_FILE="$(mktemp)"
trap 'rm -f "$AGENT_LIST_FILE"' EXIT
multica agent list --output json > "$AGENT_LIST_FILE" || die "multica agent list 失败"

# resolve_agent_id <name> -> prints agent id (exit 1 if not found, 2 if archived)
resolve_agent_id() {
  local name="$1"
  NAME="$name" AGENT_LIST_FILE="$AGENT_LIST_FILE" python3 -c '
import json, os, sys
name = os.environ["NAME"]
with open(os.environ["AGENT_LIST_FILE"]) as f:
    agents = json.load(f)
matches = [a for a in agents if a.get("name") == name]
if not matches:
    sys.exit(1)
if matches[0].get("archived_at"):
    sys.stderr.write("agent %r is archived\n" % name); sys.exit(2)
print(matches[0]["id"])
'
}

# find_squad_id <name> -> prints squad id or empty
find_squad_id() {
  local name="$1"
  multica squad list --output json 2>/dev/null | NAME="$name" python3 -c '
import json, os, sys
name = os.environ["NAME"]
try:
    squads = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for s in squads:
    if s.get("name") == name and not s.get("archived_at"):
        print(s["id"]); break
' || true
}

# member_exists <squad-id> <member-type> <member-id> -> 0/1
member_exists() {
  local squad_id="$1" mtype="$2" mid="$3"
  multica squad member list "$squad_id" --output json 2>/dev/null | \
    MTYPE="$mtype" MID="$mid" python3 -c '
import json, os, sys
mtype, mid = os.environ["MTYPE"], os.environ["MID"]
try:
    members = json.load(sys.stdin)
except Exception:
    sys.exit(1)
for m in members:
    if m.get("member_type") == mtype and m.get("member_id") == mid:
        sys.exit(0)
sys.exit(1)
'
}

# ---------- main ----------

echo "==> 解析成员 agent id（按名字）"
leader_name="${MEMBERS[0]%%|*}"
leader_id=""
member_count=0
MEMBER_NAMES=()
MEMBER_IDS=()
MEMBER_TYPES=()
MEMBER_ROLES=()
for entry in "${MEMBERS[@]}"; do
  name="${entry%%|*}"
  rest="${entry#*|}"
  mtype="${rest%%|*}"
  role="${rest##*|}"
  id="$(resolve_agent_id "$name")" || die "agent 不存在: $name（请在 Agents 页创建或改名后重跑）"
  MEMBER_NAMES+=("$name"); MEMBER_IDS+=("$id"); MEMBER_TYPES+=("$mtype"); MEMBER_ROLES+=("$role")
  echo "    $name ($mtype, $role) -> ${id:0:8}…"
done
leader_id="${MEMBER_IDS[0]}"
echo "==> leader = $leader_name ($leader_id)"

echo "==> 查找或创建 squad「$SQUAD_NAME」"
squad_id="$(find_squad_id "$SQUAD_NAME")"
if [ -n "$squad_id" ]; then
  echo "    已存在，复用 $squad_id"
  cur_leader="$(multica squad get "$squad_id" --output json 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("leader_id",""))' 2>/dev/null || true)"
  if [ -n "$cur_leader" ] && [ "$cur_leader" != "$leader_id" ]; then
    echo "    校正 leader -> $leader_name"
    multica squad update "$squad_id" --leader "$leader_id" >/dev/null
  fi
else
  echo "    创建中…"
  squad_id="$(multica squad create --name "$SQUAD_NAME" --leader "$leader_id" --description "$SQUAD_DESC" --output json | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')"
  echo "    创建完成 $squad_id"
fi

echo "==> 写入 squad 指令"
multica squad update "$squad_id" --instructions "$INSTRUCTIONS" >/dev/null
echo "    OK"

echo "==> 加入成员（幂等，已存在则跳过）"
i=0
for entry in "${MEMBERS[@]}"; do
  name="${MEMBER_NAMES[$i]}"; mid="${MEMBER_IDS[$i]}"; mtype="${MEMBER_TYPES[$i]}"; role="${MEMBER_ROLES[$i]}"
  if member_exists "$squad_id" "$mtype" "$mid"; then
    echo "    $name 已在 squad，跳过"
  else
    multica squad member add "$squad_id" --type "$mtype" --member-id "$mid" --role "$role" >/dev/null
    echo "    + $name ($role)"
  fi
  i=$((i+1))
done

echo
echo "==> 完成。demo squad id：$squad_id"
echo
echo "下一步（启动 5 分钟演示）："
echo "  multica issue create \\"
echo "    --title \"Demo: 给用户菜单增加深色模式开关\" \\"
echo "    --description \"演示用，由代码交付小队协作完成。\" \\"
echo "    --assignee \"$SQUAD_NAME\""
echo
echo "查看成员状态："
echo "  multica squad get $squad_id"
echo "  multica squad member list $squad_id --output json"
echo
echo "完整步骤见 docs/demo/squad-collaboration.md"

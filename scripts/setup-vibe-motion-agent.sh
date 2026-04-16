#!/bin/bash
set -e

# Setup script for vibe-motion agent in Multica
# Creates 5 skills and 1 agent, then assigns skills to the agent

API_BASE="${MULTICA_API_BASE:-http://localhost:8080}"
WORKSPACE_ID="${MULTICA_WORKSPACE_ID:?Set MULTICA_WORKSPACE_ID}"
USER_ID="${MULTICA_USER_ID:?Set MULTICA_USER_ID}"
JWT_SECRET="${JWT_SECRET:?Set JWT_SECRET}"
RUNTIME_ID="${MULTICA_RUNTIME_ID:?Set MULTICA_RUNTIME_ID}"

# Generate JWT
TOKEN=$(python3 -c "import jwt,time; print(jwt.encode({'sub':'$USER_ID','exp':int(time.time())+86400},'$JWT_SECRET',algorithm='HS256'))" 2>/dev/null)

AUTH_HEADERS=(-H "Authorization: Bearer $TOKEN" -H "X-Workspace-ID: $WORKSPACE_ID" -H "Content-Type: application/json")

echo "=== Creating Skills ==="

# 1. remotion-best-practices
echo "Creating skill: remotion-best-practices..."
SKILL1=$(curl -s -X POST "$API_BASE/api/skills" "${AUTH_HEADERS[@]}" -d @- <<'SKILL_EOF'
{
  "name": "remotion-best-practices",
  "description": "Remotion 框架最佳实践知识库 — 视频创作 React 框架。涵盖动画、合成、字幕、3D、音频可视化、图表、GIF、转场、透明视频渲染等全部领域知识。",
  "content": "# Remotion Best Practices\n\n## When to use\n\nUse this skill whenever you are dealing with Remotion code to obtain domain-specific knowledge.\n\n## New project setup\n\n```bash\nnpx create-video@latest --yes --blank --no-tailwind my-video\n```\n\n## Starting preview\n\n```bash\nnpx remotion studio\n```\n\n## One-frame render check\n\n```bash\nnpx remotion still [composition-id] --scale=0.25 --frame=30\n```\n\n## Available Rules\n\n- 3d.md - 3D content using Three.js and React Three Fiber\n- animations.md - Fundamental animation techniques\n- assets.md - Importing images, videos, audio, and fonts\n- audio.md - Audio handling (import, trim, volume, speed, pitch)\n- calculate-metadata.md - Dynamic composition duration/dimensions/props\n- charts.md - Chart and data visualization patterns\n- compositions.md - Defining compositions, stills, folders, default props\n- fonts.md - Loading Google Fonts and local fonts\n- gifs.md - GIF playback synchronized with timeline\n- lottie.md - Lottie animation embedding\n- sequencing.md - Delay, trim, limit duration patterns\n- transitions.md - Scene transition patterns\n- transparent-videos.md - Transparent video rendering\n- trimming.md - Animation trimming patterns\n- videos.md - Video embedding (trim, volume, speed, loop, pitch)\n- parameters.md - Parametrizable videos with Zod schema\n- text-animations.md - Typography and text animation patterns\n- timing.md - interpolate, Bézier easing, springs\n- maps.md - Mapbox map animations\n- silence-detection.md - Adaptive silence detection via FFmpeg\n- voiceover.md - AI-generated voiceover via ElevenLabs TTS",
  "config": {"tags": ["remotion", "video", "react", "animation", "composition"]}
}
SKILL_EOF
)
SKILL1_ID=$(echo "$SKILL1" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Created: $SKILL1_ID"

# 2. svg-assembly-animator
echo "Creating skill: svg-assembly-animator..."
SKILL2=$(curl -s -X POST "$API_BASE/api/skills" "${AUTH_HEADERS[@]}" -d @- <<'SKILL_EOF'
{
  "name": "svg-assembly-animator",
  "description": "SVG 零件组装动画 — 将静态 SVG 矢量图转换为充满力量感与速度感的零件组装动画，支持导出 30fps 透明背景序列帧 ZIP。适用于 AE/PR 视频剪辑素材制作。",
  "content": "# SVG Assembly Animator\n\n将静态 SVG 转换为具有专业动效（Viscous & Dynamic 风格）的 HTML5 动画，提供透明序列帧导出。\n\n## 核心工作流\n\n1. **分析 SVG 结构**：识别主体（第一个 path）和细节零件（剩余 path）\n2. **准备 HTML 模板**：使用 GSAP (TweenMax) 动画引擎 + JSZip + Canvas 导出\n3. **实现动画逻辑**：\n   - 打散阶段：反向逻辑，零件随机抛射 + 极端轴向缩放（ScaleX: 0.05, ScaleY: 4）\n   - 组装阶段：主体 elastic.out 浮现 → 零件 stagger 飞入 back.out 回弹 → 整体旋转缩放\n4. **生成交付**：.html 文件，浏览器预览 + 导出按钮\n\n## 动画参数\n\n- 标准组装：duration: 1.2, ease: back.out(2.5)\n- 力量组装：duration: 0.7, ease: back.out(5), rotation: -360\n- 优雅组装：duration: 1.5, ease: power4.out, stagger: 0.02\n\n## 导出\n\n- 默认 1080x1080, 30fps 透明 PNG 序列帧 ZIP\n- 合成视频：`ffmpeg -framerate 30 -i frame_%04d.png -vcodec prores_ks -profile:v 4 -pix_fmt yuva444p10le output.mov`",
  "config": {"tags": ["svg", "animation", "gsap", "sequence-frames", "transparent"]}
}
SKILL_EOF
)
SKILL2_ID=$(echo "$SKILL2" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Created: $SKILL2_ID"

# 3. claude-typer
echo "Creating skill: claude-typer..."
SKILL3=$(curl -s -X POST "$API_BASE/api/skills" "${AUTH_HEADERS[@]}" -d @- <<'SKILL_EOF'
{
  "name": "claude-typer",
  "description": "Claude 风格打字机动画 — 渲染提示词打字动画视频。通过 Remotion CLI 调用远程站点渲染，输出 30fps 透明 ProRes 4444 MOV。使用场景：做一个 Claude 打字动画、创建提示词动画。",
  "content": "# Claude Typer\n\n## Workflow\n\n1. 提取用户输入的 prompt 文本\n2. 运行渲染脚本：\n   ```bash\n   skill_dir=\"\"\n   for base in \"${AGENTS_HOME:-$HOME/.agents}\" \"${CLAUDE_HOME:-$HOME/.claude}\" \"${CODEX_HOME:-$HOME/.codex}\"; do\n     if [ -d \"$base/skills/claude-typer\" ]; then\n       skill_dir=\"$base/skills/claude-typer\"\n       break\n     fi\n   done\n   /usr/local/bin/python3 \"$skill_dir/scripts/render_claude_typer.py\" \"<prompt>\"\n   ```\n3. 返回生成的视频路径\n\n## 渲染参数\n\n- 远程合成 Typer30fps from https://www.laosunwendao.com\n- 优先 bunx @remotion/cli，fallback npx\n- 透明 MOV：--fps=30, --codec=prores, --prores-profile=4444, --pixel-format=yuva444p10le\n- --scale=2, --timeout=300000, --concurrency=1\n\n## 可配置 Props\n\nprompt, typingSpeedMs, model, videoWidth, videoHeight, claudeWidth, tiltStartX/Y, tiltEndX/Y, tiltDurationRatio\n\n## 输出命名\n\n自动从 prompt 提取中英文关键词作为文件名，例如 \"帮我做一个web画板\" → web画板.mov",
  "config": {"tags": ["claude", "typing", "animation", "remotion", "video"]}
}
SKILL_EOF
)
SKILL3_ID=$(echo "$SKILL3" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Created: $SKILL3_ID"

# 4. ruler-progress-render
echo "Creating skill: ruler-progress-render..."
SKILL4=$(curl -s -X POST "$API_BASE/api/skills" "${AUTH_HEADERS[@]}" -d @- <<'SKILL_EOF'
{
  "name": "ruler-progress-render",
  "description": "尺子进度条动画 — 克隆/更新 ruler-progress-animator 项目并渲染尺子进度条动画视频。使用场景：绘制尺子进度条、做尺子进度动画、渲染 ruler progress。",
  "content": "# Ruler Progress Render\n\n## Workflow\n\n1. 使用 skill 中的 `scripts/render_ruler_progress.sh`\n2. 第一个参数为 workspace_dir（默认当前目录），第二个参数为 output_path\n\n```bash\nbash scripts/render_ruler_progress.sh [workspace_dir] [output_path]\n```\n\n## Behavior\n\n- 本地存在 ruler-progress-animator 则复用，否则从 GitHub 克隆\n- 跟踪远程默认分支（origin/HEAD）更新\n- 安装 npm 依赖\n- 有 bunx 时使用 scaffold flow：remotion:ensure-browser + remotion:render\n- 无 bunx 时 fallback 到 npx remotion render\n- 默认输出：out/scaffold-demo-defaults-transparent.mov\n\n## Requirements\n\ngit, node, npm, network access",
  "config": {"tags": ["ruler", "progress", "animation", "remotion"]}
}
SKILL_EOF
)
SKILL4_ID=$(echo "$SKILL4" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Created: $SKILL4_ID"

# 5. procedural-fish-render
echo "Creating skill: procedural-fish-render..."
SKILL5=$(curl -s -X POST "$API_BASE/api/skills" "${AUTH_HEADERS[@]}" -d @- <<'SKILL_EOF'
{
  "name": "procedural-fish-render",
  "description": "程序鱼动画渲染 — 克隆/更新 vibe-motion/procedural-fish 项目并渲染程序化鱼类动画视频。使用场景：渲染程序鱼、导出程序鱼视频、procedural fish animation。",
  "content": "# Procedural Fish Render\n\n## Workflow\n\n1. 解析 skill_dir 并运行渲染脚本：\n   ```bash\n   skill_dir=\"\"\n   for base in \"${AGENTS_HOME:-$HOME/.agents}\" \"${CLAUDE_HOME:-$HOME/.claude}\" \"${CODEX_HOME:-$HOME/.codex}\"; do\n     if [ -d \"$base/skills/procedural-fish-render\" ]; then\n       skill_dir=\"$base/skills/procedural-fish-render\"\n       break\n     fi\n   done\n   /usr/local/bin/python3 \"$skill_dir/scripts/render_procedural_fish.py\"\n   ```\n2. 可选参数：--workspace, --output, --props-file\n3. 返回最终视频的绝对路径\n\n## Behavior\n\n- 仓库源：https://github.com/vibe-motion/procedural-fish\n- 本地存在则 git fetch + checkout main + pull --ff-only\n- 不存在则 clone\n- 渲染命令：pnpm run remotion:render\n- 默认输出：out/procedural-fish-transparent.mov\n- 默认 props：shared/project/render-presets/default.json",
  "config": {"tags": ["procedural", "fish", "animation", "remotion"]}
}
SKILL_EOF
)
SKILL5_ID=$(echo "$SKILL5" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Created: $SKILL5_ID"

echo ""
echo "=== Creating Agent: Vibe Motion ==="

AGENT=$(curl -s -X POST "$API_BASE/api/agents" "${AUTH_HEADERS[@]}" -d @- <<AGENT_EOF
{
  "name": "Vibe Motion",
  "description": "动画与视频渲染专家 — 整合 Remotion 最佳实践、SVG 组装动画、Claude 打字机动画、尺子进度条、程序鱼等 5 个 vibe-motion 技能，专注 Motion Graphics 视频制作与渲染。",
  "instructions": "You are **Vibe Motion Agent** — a specialist in motion graphics, animation, and video rendering.\n\n## Your Skills\n\nYou have 5 animation skills:\n\n1. **remotion-best-practices** — Remotion 框架领域知识库（动画、合成、字幕、3D、音频、图表、GIF、转场等），开发 Remotion 项目时先查阅\n2. **svg-assembly-animator** — 将静态 SVG 转换为 GSAP 驱动的零件组装动画，支持 30fps 透明序列帧导出\n3. **claude-typer** — 渲染 Claude 风格提示词打字机动画视频（远程 Remotion 合成，ProRes 4444 透明输出）\n4. **ruler-progress-render** — 渲染尺子进度条动画视频（基于 ruler-progress-animator 项目）\n5. **procedural-fish-render** — 渲染程序化鱼类动画视频（基于 vibe-motion/procedural-fish 项目）\n\n## Workflow\n\n1. 分析用户的动画/视频需求，匹配最合适的 skill\n2. 读取对应 skill 的内容获取完整工作流和参数\n3. 按 skill 指定的脚本和命令执行渲染\n4. 报告输出文件路径和预览方式\n\n## Constraints\n\n- 先读取 skill 内容再操作，不要猜测参数\n- 涉及 Remotion 开发时，始终参考 remotion-best-practices\n- 不要修改 skill 脚本除非用户明确要求\n- 渲染完成后务必报告输出文件绝对路径",
  "runtime_id": "$RUNTIME_ID",
  "visibility": "workspace",
  "max_concurrent_tasks": 2
}
AGENT_EOF
)
AGENT_ID=$(echo "$AGENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Agent ID: $AGENT_ID"

echo ""
echo "=== Assigning Skills to Agent ==="

curl -s -X PUT "$API_BASE/api/agents/$AGENT_ID/skills" "${AUTH_HEADERS[@]}" -d @- <<ASSIGN_EOF
{
  "skill_ids": ["$SKILL1_ID", "$SKILL2_ID", "$SKILL3_ID", "$SKILL4_ID", "$SKILL5_ID"]
}
ASSIGN_EOF
echo ""

echo ""
echo "=== Done ==="
echo "Agent: Vibe Motion ($AGENT_ID)"
echo "Skills assigned:"
echo "  1. remotion-best-practices ($SKILL1_ID)"
echo "  2. svg-assembly-animator ($SKILL2_ID)"
echo "  3. claude-typer ($SKILL3_ID)"
echo "  4. ruler-progress-render ($SKILL4_ID)"
echo "  5. procedural-fish-render ($SKILL5_ID)"
echo ""
echo "Open http://localhost:3000 to see the agent in the Agents page."

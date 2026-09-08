#!/usr/bin/env python3
"""Heuristic Work-Finder: append agent-safe tickets when queue is thin.

Does not invent new product lines. Avoids wont_do keywords and backlog duplicates.
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

TICKET_RE = re.compile(
    r"^###\s+(TICKET-[A-Z]*\d+)\s+\[(agent-safe|agent-assisted|human-only)\]\s+(.+)$"
)
CHECK_RE = re.compile(r"^- \[[ xX]\]\s+(.+)$")

# stack -> list of (title, what, dod)
CATALOG: dict[str, list[tuple[str, str, str]]] = {
    "cloudflare-pages": [
        ("404 页与基础 metadata", "增加最小 not-found / 404 文案与站点 title metadata", "`pnpm typecheck` exit 0"),
        ("页脚 Privacy/Terms 链接补齐", "footer 链到 privacy/terms（页不存在则先占位）", "`make check` 或 `pnpm typecheck` exit 0"),
        ("OG / Twitter card meta", "首页补充 openGraph / twitter meta 占位", "`pnpm typecheck` exit 0"),
        ("无障碍：主按钮 aria-label", "主 CTA / 关键按钮补 aria-label 与键盘可达", "`pnpm test` 或 `pnpm typecheck` exit 0"),
        ("静态约页性能：图片 alt", "关键图片补有意义 alt；无图则跳过改文案区 landmark", "`pnpm typecheck` exit 0"),
    ],
    "next-static": [
        ("404 页与 metadata", "`not-found` 最小页 + metadata title", "`pnpm typecheck` exit 0"),
        ("robots.txt 与 sitemap 占位", "public/robots.txt + 最小 sitemap 路由/文件", "`pnpm typecheck` exit 0"),
        ("favicon 与 web manifest", "favicon + site.webmanifest（name/theme_color）", "`pnpm typecheck` exit 0"),
        ("OG tags 补齐", "layout metadata 增加 openGraph 字段", "`pnpm typecheck` exit 0"),
        ("页脚合规链接", "footer 链到 Privacy/Terms", "`pnpm typecheck` exit 0"),
    ],
    "next-go-saas": [
        ("GET /health 存活探针（若缺失）", "server 增加/加固 GET /health → {\"status\":\"ok\"}", "`make check` exit 0"),
        ("OpenAPI 与 handler 对齐小补丁", "为已有 mock 路径补 openapi 或测试断言", "`make check` exit 0"),
        ("Dashboard 空态文案", "dashboard 无数据时展示友好空态（无真实 API）", "`pnpm typecheck` exit 0"),
        ("Settings 只读字段校验文案", "settings 占位表单增加 client-side 校验提示", "`pnpm typecheck` exit 0"),
        ("营销页 SEO metadata", "营销页 title/description/OG 占位", "`pnpm typecheck` exit 0"),
    ],
    "vite-react-beatscape": [
        ("路由级 ErrorBoundary 占位", "关键路由包一层错误边界与重试文案", "相关 test 或 `pnpm test` exit 0"),
        ("加载态 skeleton 占位", "列表/主面板增加最小 loading UI", "`pnpm test` 或 typecheck exit 0"),
        ("无障碍：主控件 label", "主交互控件补 aria-label", "`pnpm test` exit 0"),
        ("环境变量示例文档对齐", "README 或 .env.example 与真实键名对齐（不写密钥）", "文档 diff 可审；`pnpm test` 不红"),
    ],
    "visual-replica": [
        ("Gallery 卡片 detail overlay", "卡片点击打开 detail overlay；375 宽可关闭", "`make visual-check` exit 0"),
        ("Locale 持久化 zh/en", "localStorage 记住语言；刷新保持", "`make check` exit 0"),
        ("Skills 向导键盘与 a11y", "Skills 步骤 aria-label；Esc 关闭", "`make check` exit 0"),
        ("Studio mobile 375 布局", "`/#studio` 单列 dock 不溢出", "`make visual-check` exit 0"),
        ("OG / Twitter meta 占位", "index.html title/description/OG 字段", "`make check` exit 0"),
        ("404 与基础 metadata", "最小 404 文案与 title metadata", "`make check` exit 0"),
    ],
}

DEFAULT_CATALOG = CATALOG["cloudflare-pages"]


def read_text(path: Path) -> str:
    if not path.is_file():
        return ""
    return path.read_text(encoding="utf-8")


def parse_tickets(backlog: str) -> list[tuple[str, str, str]]:
    out: list[tuple[str, str, str]] = []
    for line in backlog.splitlines():
        m = TICKET_RE.match(line.strip())
        if m:
            out.append((m.group(1), m.group(2), m.group(3).strip()))
    return out


def next_ticket_num(tickets: list[tuple[str, str, str]]) -> int:
    best = 0
    for tid, _, _ in tickets:
        m = re.search(r"(\d+)$", tid)
        if m:
            best = max(best, int(m.group(1)))
    return best + 1


def title_blob(tickets: list[tuple[str, str, str]], backlog: str) -> str:
    return (" ".join(t[2] for t in tickets) + "\n" + backlog).lower()


def wont_do_blocked(title: str, what: str, wont: str) -> bool:
    if not wont.strip():
        return False
    blob = (title + " " + what).lower()
    # crude: if a wont_do checkbox line shares 2+ significant tokens with proposal
    for line in wont.splitlines():
        line_l = line.lower()
        if "支付" in line_l or "payment" in line_l:
            if any(k in blob for k in ("支付", "payment", "stripe", "checkout")):
                return True
        if "登录" in line_l or "auth" in line_l or "oauth" in line_l:
            if any(k in blob for k in ("登录", "oauth", "auth0", "真实登录")):
                return True
        if "密钥" in line_l or "secret" in line_l:
            if any(k in blob for k in ("密钥", "secret", "api key")):
                return True
    return False


def already_covered(title: str, blob: str) -> bool:
    # keyword overlap against existing backlog
    keys = [w for w in re.split(r"[\s/：:，,]+", title.lower()) if len(w) >= 3]
    if not keys:
        return False
    hits = sum(1 for k in keys if k in blob)
    return hits >= max(2, len(keys) // 2)


def unchecked_acs(accept: str) -> list[str]:
    out: list[str] = []
    for line in accept.splitlines():
        m = CHECK_RE.match(line.strip())
        if not m:
            continue
        item = m.group(1).strip()
        # skip checked — pattern was [ ] or [x]; only want empty
        if line.strip().lower().startswith("- [x]"):
            continue
        if line.strip().startswith("- [ ]") or line.strip().startswith("- [ ]"):
            out.append(item)
        elif re.match(r"^- \[ \]", line.strip()):
            out.append(item)
    # fix: only unchecked
    out = []
    for line in accept.splitlines():
        s = line.strip()
        if s.startswith("- [ ]"):
            out.append(s[5:].strip())
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--backlog", required=True)
    ap.add_argument("--slug", required=True)
    ap.add_argument("--stack", default="cloudflare-pages")
    ap.add_argument("--max", type=int, default=2)
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--date", default="")
    args = ap.parse_args()

    backlog_path = Path(args.backlog)
    if not backlog_path.is_file():
        print(f"error: backlog missing: {backlog_path}", file=sys.stderr)
        return 1

    root = backlog_path.parent
    backlog = read_text(backlog_path)
    accept = read_text(root / "accept_cases.md")
    wont = read_text(root / "wont_do.md")
    tickets = parse_tickets(backlog)
    blob = title_blob(tickets, backlog)
    n = next_ticket_num(tickets)
    date = args.date or __import__("datetime").date.today().isoformat()

    proposals: list[tuple[str, str, str]] = []

    for ac in unchecked_acs(accept)[: args.max]:
        title = f"补齐验收：{ac[:60]}"
        what = f"实现或加固未勾选验收项：{ac}"
        dod = f"对应 accept_cases 项可验证；`make check` 或项目默认 check exit 0"
        if wont_do_blocked(title, what, wont) or already_covered(title, blob):
            continue
        proposals.append((title, what, dod))

    catalog = CATALOG.get(args.stack, DEFAULT_CATALOG)
    for title, what, dod in catalog:
        if len(proposals) >= args.max:
            break
        if wont_do_blocked(title, what, wont) or already_covered(title, blob):
            continue
        proposals.append((title, what, dod))

    proposals = proposals[: args.max]
    if not proposals:
        print("WORK_FINDER_HEURISTIC added=0")
        return 0

    blocks: list[str] = []
    for i, (title, what, dod) in enumerate(proposals):
        tid = f"TICKET-{n + i:03d}"
        blocks.append(
            "\n".join(
                [
                    f"### {tid} [agent-safe] {title}",
                    "",
                    "- **Owner:** Implementer",
                    f"- **What:** {what}",
                    f"- **AC / DoD:** {dod}",
                    f"- **Source:** work-finder heuristic {date}",
                    "",
                ]
            )
        )

    text = "\n" + "\n".join(blocks)
    if args.dry_run:
        print(text)
        print(f"WORK_FINDER_HEURISTIC added={len(proposals)} (dry-run)")
        return 0

    with backlog_path.open("a", encoding="utf-8") as f:
        if not backlog.endswith("\n"):
            f.write("\n")
        f.write(text)
    print(f"WORK_FINDER_HEURISTIC added={len(proposals)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

/** Self-contained console page; it talks only to this daemon's console API. */
export const consolePage = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Multica 验证管理台</title>
<style>
  :root { color-scheme: light dark; --fg:#1f2328; --muted:#656d76; --border:#d0d7de; --bg:#fff; --card:#f6f8fa;
    --ok:#1a7f37; --warn:#9a6700; --bad:#cf222e; }
  @media (prefers-color-scheme: dark) { :root { --fg:#e6edf3; --muted:#8d96a0; --border:#30363d; --bg:#0d1117; --card:#161b22;
    --ok:#3fb950; --warn:#d29922; --bad:#f85149; } }
  * { box-sizing:border-box } body { margin:0; font:14px/1.5 system-ui,-apple-system,sans-serif; color:var(--fg); background:var(--bg) }
  header { padding:16px 24px; border-bottom:1px solid var(--border); display:flex; gap:16px; align-items:baseline; flex-wrap:wrap }
  h1 { font-size:18px; margin:0 } h2 { font-size:15px; margin:24px 0 8px }
  main { padding:0 24px 32px; max-width:1200px }
  .muted { color:var(--muted) } code { font:12px ui-monospace,monospace; overflow-wrap:anywhere }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:12px }
  .card { background:var(--card); border:1px solid var(--border); border-radius:8px; padding:12px }
  .card .v { font-size:16px; margin-top:4px }
  table { width:100%; border-collapse:collapse } th,td { text-align:left; padding:8px; border-bottom:1px solid var(--border); vertical-align:top }
  th { font-weight:600; color:var(--muted); font-size:12px }
  .pill { display:inline-block; padding:0 8px; border-radius:999px; font-size:12px; border:1px solid currentColor }
  .ok { color:var(--ok) } .warn { color:var(--warn) } .bad { color:var(--bad) }
  .bars { display:flex; gap:1px; align-items:flex-end; height:18px } .bars i { width:4px; height:100%; border-radius:1px; opacity:.85 }
  .chips span { margin-right:8px; font-size:12px }
  button { font:inherit; padding:2px 10px; border-radius:6px; border:1px solid var(--border); background:var(--bg); color:var(--fg); cursor:pointer }
  button:hover { border-color:var(--muted) }
  pre { background:var(--card); border:1px solid var(--border); border-radius:8px; padding:12px; max-height:420px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font:12px ui-monospace,monospace }
</style>
</head>
<body>
<header><h1>Multica 验证管理台</h1><span class="muted" id="daemon"></span></header>
<main id="app">
  <h2>设备</h2><div class="grid" id="device"></div>
  <h2>环境</h2><table><thead><tr><th>环境</th><th>状态</th><th>健康（近 15 分钟）</th><th>组件</th><th>空闲回收</th><th></th></tr></thead><tbody id="envs"></tbody></table>
  <h2>任务</h2><table><thead><tr><th>操作</th><th>状态</th><th>阶段</th><th>资源</th><th>更新时间</th><th>问题</th></tr></thead><tbody id="jobs"></tbody></table>
  <h2>日志</h2>
  <div><select id="log-env"></select> <select id="log-name"><option>prepare</option><option>backend</option><option>desktop</option><option>stop</option></select> <button id="log-load">读取</button></div>
  <pre id="log" class="muted">选择环境和日志后读取。</pre>
</main>
<script>
const $ = (id) => document.getElementById(id);
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[c]);
const short = (id) => esc(String(id).slice(0, 8));
const time = (iso) => iso ? new Date(iso).toLocaleTimeString() : "—";
const duration = (ms) => ms == null ? "—" : ms < 60000 ? Math.round(ms / 1000) + " 秒" : Math.round(ms / 60000) + " 分钟";
const tone = (s) => ({ healthy:"ok", ready:"ok", completed:"ok", up:"ok", running:"warn", starting:"warn", awaitingUser:"warn", queued:"warn",
  degraded:"warn", preparing:"warn", stopping:"warn", failed:"bad", down:"bad", interrupted:"bad", unknown:"bad", cancelled:"muted", stopped:"muted" })[s] || "muted";
const pill = (s) => '<span class="pill ' + tone(s) + '">' + esc(s) + '</span>';
async function api(path, init) {
  const response = await fetch("/console/api/" + path, init);
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}
function renderDevice(o) {
  const d = o.device;
  $("daemon").innerHTML = "MCP <code>" + esc(o.daemon.mcpUrl) + "</code> · pid " + esc(o.daemon.pid) + " · 已运行 " + duration(Date.now() - Date.parse(o.daemon.startedAt)) + " · " + esc(o.daemon.commit.slice(0, 9));
  const cards = d ? [
    ["磁盘可用", (d.diskFreeBytes / 1024 ** 3).toFixed(1) + " GB", d.diskFreeBytes > 8 * 1024 ** 3 ? "ok" : "bad"],
    ["Docker", d.docker.status === "up" ? esc(d.docker.detail) : "不可用", tone(d.docker.status)],
    ["已安装的 Multica", d.installedMultica.running ? "运行中（pid " + d.installedMultica.pid + "）" : "未运行", "muted"],
    ["环境名额", d.capacity.running + " / " + d.capacity.max + " 运行中", d.capacity.running >= d.capacity.max ? "bad" : "ok"],
  ] : [["设备", "采样中…", "muted"]];
  $("device").innerHTML = cards.map(([k, v, t]) => '<div class="card"><div class="muted">' + k + '</div><div class="v ' + t + '">' + v + '</div></div>').join("");
}
function renderEnvironments(o) {
  const rows = o.environments.map((e) => {
    const env = e.environment, last = e.health[e.health.length - 1];
    const bars = e.health.map((h) => '<i class="' + tone(h.status) + '" style="background:currentColor" title="' + esc(time(h.at) + " " + h.status) + '"></i>').join("");
    const chips = last ? Object.entries(last.components).map(([k, v]) => '<span class="' + tone(v.status) + '" title="' + esc(v.detail || "") + '">' + esc(k) + (v.latencyMs != null ? " " + v.latencyMs + "ms" : "") + '</span>').join("") : '<span class="muted">—</span>';
    const stop = ["stopped", "failed"].includes(env.state) ? "" : '<button data-stop="' + esc(env.environmentId) + '">停止</button>';
    return "<tr><td><code>" + short(env.environmentId) + "</code>" + (env.owner ? " " + esc(env.owner) : "") + "<div class=muted>" + esc((env.branch || "") + " @ " + env.commit.slice(0, 9)) + "</div><div class=muted>" + esc(env.mode + " · " + env.dataMode + " · " + env.apiOrigin) + "</div></td>"
      + "<td>" + pill(env.state) + (env.readiness ? "<div class=muted>" + esc(env.readiness) + "</div>" : "") + (env.problem ? '<div class="bad">' + esc(env.problem.message) + "</div>" : "") + "</td>"
      + '<td><div class="bars">' + (bars || '<span class="muted">—</span>') + "</div>" + (last ? pill(last.status) : "") + "</td>"
      + '<td class="chips">' + chips + "</td><td>" + duration(e.idleRemainingMs) + "</td><td>" + stop + "</td></tr>";
  });
  $("envs").innerHTML = rows.join("") || '<tr><td colspan="6" class="muted">没有环境。智能体通过 MCP 调用 env_start 创建。</td></tr>';
  const selected = $("log-env").value;
  $("log-env").innerHTML = o.environments.map((e) => '<option value="' + esc(e.environment.environmentId) + '">' + short(e.environment.environmentId) + "</option>").join("");
  if (selected) $("log-env").value = selected;
}
function renderJobs(jobs) {
  $("jobs").innerHTML = jobs.map((j) => "<tr><td><code>" + esc(j.operation) + "</code></td><td>" + pill(j.state) + "</td><td>" + esc(j.phase) + "</td><td>" + j.resourceRefs.map(short).join(", ") + "</td><td>" + time(j.updatedAt) + "</td><td>" + esc(j.problem ? j.problem.message : "") + "</td></tr>").join("") || '<tr><td colspan="6" class="muted">没有任务。</td></tr>';
}
async function refresh() {
  try {
    const [overview, jobs] = await Promise.all([api("overview"), api("jobs")]);
    renderDevice(overview); renderEnvironments(overview); renderJobs(jobs);
  } catch (error) { console.error(error); }
}
document.addEventListener("click", async (event) => {
  const id = event.target.dataset && event.target.dataset.stop;
  if (!id || !confirm("停止环境 " + id.slice(0, 8) + "？会停止它的进程并删除数据库容器和 worktree。")) return;
  await api("environments/" + encodeURIComponent(id) + "/stop", { method: "POST" });
  refresh();
});
$("log-load").onclick = async () => {
  const id = $("log-env").value; if (!id) return;
  try {
    const page = await api("logs?environmentId=" + encodeURIComponent(id) + "&name=" + $("log-name").value);
    $("log").textContent = (page.truncated ? "[日志已截断]\\n" : "") + page.text || "（空）";
  } catch (error) { $("log").textContent = "无法读取：" + error.message; }
};
refresh(); setInterval(refresh, 5000);
</script>
</body>
</html>`;

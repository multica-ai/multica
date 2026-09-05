const state = {
  projects: [],
  totals: {},
  overview: null,
  selectedRepo: "",
  selectedProject: null,
  meta: {},
  runtime: {},
  jobs: [],
  pollTimer: null,
};

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `Request failed: ${response.status}`);
  }
  return data;
}

function statusClass(project) {
  if (!project.accessible) return "muted";
  if (Number(project.blocked) > 0) return "bad";
  if (project.paused === true || project.paused === "true") return "warn";
  return "good";
}

function renderContentCockpit() {
  const content = state.overview?.content;
  if (!content) return;

  const hqLink = document.getElementById("content-hq-link");
  const agentLink = document.getElementById("content-agent-link");
  if (hqLink) hqLink.href = content.workbench_url || "https://hq.revoices.app/#content/review";
  if (agentLink) agentLink.href = content.agent_url || "https://agent.revoices.app/";

  const hint = document.getElementById("content-remote-hint");
  if (hint) {
    hint.innerHTML = `执行在 <code>ssh ${content.remote_ssh || "lighthouse"}</code>；队列真相源 GitHub Issues；审稿在 hq.revoices.app（Kanban 仅历史，不作主队列）。`;
  }

  const totals = content.totals || {};
  const metrics = document.getElementById("content-metrics");
  if (metrics) {
    metrics.innerHTML = `
      <div class="card"><div class="metric-label">内容 BLOCKED</div><div class="metric-value">${totals.blocked ?? 0}</div></div>
      <div class="card"><div class="metric-label">内容 RUNNING</div><div class="metric-value">${totals.running ?? 0}</div></div>
      <div class="card"><div class="metric-label">内容 QUEUE</div><div class="metric-value">${totals.agent_safe ?? 0}</div></div>
    `;
  }

  const lines = content.lines || [];
  const tbody = document.getElementById("content-body");
  if (!tbody) return;
  if (!lines.length) {
    tbody.innerHTML = `<tr><td colspan="4" class="cell-muted">registry 无 kind:content 项目 — 见 project-registry.yaml</td></tr>`;
    return;
  }
  tbody.innerHTML = lines
    .map((line) => {
      const wb = line.workbench_url || content.workbench_url;
      const mode = line.dispatch_mode || "remote-pull";
      const channels = (line.channels || []).join(", ") || "—";
      return `<tr>
        <td>
          <div class="project-name">${line.id}${line.paused ? " ⏸" : ""}</div>
          <div class="project-repo">${line.repo}</div>
          <div class="cell-muted">${line.notes || ""}</div>
        </td>
        <td>${mode}<div class="cell-muted">${line.executor || "remote-hermes"} · ${channels}</div></td>
        <td>B${line.blocked} R${line.running} Q${line.agent_safe}</td>
        <td><a href="${wb}" target="_blank" rel="noreferrer">审稿 HQ →</a></td>
      </tr>`;
    })
    .join("");
}

function renderOpenworldCockpit() {
  const ow = state.overview?.openworld;
  if (!ow) return;

  const hermesLink = document.getElementById("openworld-hermes-link");
  const metaLink = document.getElementById("openworld-metaviewer-link");
  if (hermesLink) hermesLink.href = ow.hermes_dashboard_url || "https://hermes.nowifiwebgames.com";
  if (metaLink) metaLink.href = ow.metadata_viewer_site || "https://www.nowifiwebgames.com";

  const hint = document.getElementById("openworld-hint");
  if (hint) {
    hint.innerHTML =
      "monorepo <code>openworld</code> + 生产站 <code>metadata-viewer</code>；本机路径经 <code>repo-paths.local.yaml</code>；装 harness 后取消 <code>paused</code> 进夜间队列。";
  }

  const totals = ow.totals || {};
  const metrics = document.getElementById("openworld-metrics");
  if (metrics) {
    metrics.innerHTML = `
      <div class="card"><div class="metric-label">OpenWorld BLOCKED</div><div class="metric-value">${totals.blocked ?? 0}</div></div>
      <div class="card"><div class="metric-label">OpenWorld RUNNING</div><div class="metric-value">${totals.running ?? 0}</div></div>
      <div class="card"><div class="metric-label">OpenWorld QUEUE</div><div class="metric-value">${totals.agent_safe ?? 0}</div></div>
    `;
  }

  const lines = ow.lines || [];
  const tbody = document.getElementById("openworld-body");
  if (!tbody) return;
  if (!lines.length) {
    tbody.innerHTML = `<tr><td colspan="4" class="cell-muted">registry 无 portfolio_group:openworld — 见 project-registry.yaml</td></tr>`;
    return;
  }
  tbody.innerHTML = lines
    .map((line) => {
      const wb = line.workbench_url || (line.id === "metadata-viewer" ? ow.metadata_viewer_site : ow.hermes_dashboard_url);
      const pathOk = line.local_path_resolved;
      const pathLabel = pathOk ? `✓ ${(line.local_path || "").split("/").slice(-2).join("/")}` : "✗ 未配置";
      const linkLabel = line.domain ? line.domain : line.id === "openworld" ? "Hermes →" : "生产站 →";
      return `<tr>
        <td>
          <div class="project-name">${line.id}${line.paused ? " ⏸" : ""}</div>
          <div class="project-repo">${line.repo}</div>
          <div class="cell-muted">${line.notes || ""}</div>
        </td>
        <td class="cell-muted">${pathLabel}</td>
        <td>B${line.blocked} R${line.running} Q${line.agent_safe}</td>
        <td><a href="${wb}" target="_blank" rel="noreferrer">${linkLabel}</a></td>
      </tr>`;
    })
    .join("");
}

function renderOverview() {
  const ov = state.overview;
  if (!ov) return;

  document.getElementById("hq-sha").textContent = `HQ ${ov.hq?.sha || "-"}`;

  const lights = [
    {
      title: "21:00 cron",
      ok: ov.process?.cron_installed,
      detail: ov.process?.cron_line || "未安装 install-nightly-cron.sh",
    },
    {
      title: "脱手验收",
      ok: ov.verify?.green === true,
      warn: ov.verify?.skipped === true,
      detail: ov.verify?.skipped
        ? "未跑（工作台点「重跑脱手验收」）"
        : `${ov.verify?.ok ?? 0} 通过 · ${ov.verify?.warn ?? 0} 警告 · ${ov.verify?.fail ?? 0} 失败`,
    },
    {
      title: "规范 manifest",
      ok: (ov.process?.manifest_paths ?? 0) > 0,
      detail: `${ov.process?.manifest_paths ?? 0} 个文件 · HQ @ ${ov.hq?.sha || "-"}`,
    },
    {
      title: "昨夜 nightly",
      ok: Boolean(ov.process?.last_nightly_log_line),
      detail: ov.process?.last_nightly_log_line || "无 ~/.multica/ceo-nightly.log",
    },
  ];

  document.getElementById("process-lights").innerHTML = lights
    .map((light) => {
      let cls = "good";
      if (light.warn) cls = "warn";
      else if (!light.ok) cls = "bad";
      return `<div class="light ${cls}"><strong>${light.title}</strong><span>${light.detail}</span></div>`;
    })
    .join("");

  const hqRoot = ov.hq?.multica_root || "";
  document.getElementById("norm-links").innerHTML = (ov.norms || [])
    .map((item) => {
      const href = hqRoot ? `file://${hqRoot}/.ai-company/${item.path}` : "#";
      return `<a href="${href}" title="${item.path}">${item.title}</a>`;
    })
    .join("");

  const tbody = document.getElementById("assets-body");
  tbody.innerHTML = (ov.projects || [])
    .map((p) => {
      const cos = p.company_os || {};
      const pathOk = p.local_path_resolved;
      let syncLabel = "未 sync";
      let syncClass = "bad";
      if (cos.synced) {
        syncLabel = cos.hq_sha_current ? `已同步 @ ${cos.hq_sha}` : `旧副本 @ ${cos.hq_sha}`;
        syncClass = cos.hq_sha_current ? "good" : "warn";
      }
      const claude = cos.claude_md ? "CLAUDE ✓" : "无 CLAUDE";
      const domain = p.domain || p.cloudflare_project || "—";
      const cf = p.cloudflare_project ? `CF: ${p.cloudflare_project}` : "";
      return `<tr>
        <td>
          <div class="project-name">${p.id}${p.paused ? " ⏸" : ""}</div>
          <div class="project-repo">${p.repo}</div>
        </td>
        <td>${p.tier || "-"}</td>
        <td class="cell-muted">${pathOk ? "✓ " + (p.local_path || "").split("/").slice(-2).join("/") : "✗ 未配置"}</td>
        <td><span class="pill ${syncClass}">${syncLabel}</span><div class="cell-muted">${claude} · ${cos.file_count || 0} 文件</div></td>
        <td class="cell-muted">${domain}${cf ? `<br>${cf}` : ""}</td>
        <td>B${p.blocked} R${p.running} Q${p.agent_safe}</td>
      </tr>`;
    })
    .join("");

  renderContentCockpit();
  renderOpenworldCockpit();
}

function formatTotals() {
  document.getElementById("metric-blocked").textContent = state.totals.blocked ?? 0;
  document.getElementById("metric-running").textContent = state.totals.running ?? 0;
  document.getElementById("metric-queue").textContent = state.totals.agent_safe ?? 0;
  document.getElementById("metric-merged").textContent = state.totals.merged_prs ?? 0;
}

function renderProjects() {
  const tbody = document.getElementById("projects-body");
  tbody.innerHTML = "";

  for (const project of state.projects) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>
        <div class="project-name">
          <span class="status-dot ${statusClass(project)}"></span>${project.id}
        </div>
        <div class="project-repo">${project.repo}</div>
      </td>
      <td>${project.blocked}</td>
      <td>${project.running}</td>
      <td>${project.agent_safe}</td>
      <td>
        <button data-repo="${project.repo}" class="view-queue">看队列</button>
      </td>
    `;
    tbody.appendChild(tr);
  }

  document.querySelectorAll(".view-queue").forEach((button) => {
    button.addEventListener("click", () => {
      const repo = button.getAttribute("data-repo");
      const project = state.projects.find((item) => item.repo === repo);
      selectProject(project);
    });
  });

  if (!state.selectedRepo && state.projects.length > 0) {
    const first = state.projects.find((item) => item.accessible && item.paused !== true && item.paused !== "true");
    if (first) selectProject(first);
  }
}

function renderMeta() {
  const pill = document.getElementById("dispatch-mode");
  const ready = state.meta.cursor_agent_ready;
  const mode = state.meta.dispatch_mode || (ready ? "local-cli" : "gha");
  pill.textContent = ready ? `${mode} · cursor-agent 已登录` : `未登录 cursor-agent · fallback ${mode}`;
  pill.className = `pill ${ready ? "good" : "warn"}`;
  pill.title = mode.includes("async")
    ? "portfolio-dispatch: nohup per slot (max_total=planned slots)"
    : "portfolio-dispatch: synchronous wait per issue";
  document.getElementById("org-label").textContent = `org: ${state.meta.org || "-"}`;
}

function renderRuntime() {
  const runtime = state.runtime || {};
  const daemonPill = document.getElementById("runtime-daemon");
  const agentsEl = document.getElementById("runtime-agents");
  const cliEl = document.getElementById("runtime-cli");

  if (!runtime.api_ok) {
    daemonPill.textContent = runtime.api_error || "Multica API 不可用";
    daemonPill.className = "pill warn";
    agentsEl.innerHTML = `<div class="empty">无法读取智能体并发</div>`;
    cliEl.textContent = "本机 cursor-agent 进程: -";
    return;
  }

  const daemon = runtime.daemon || {};
  const maxTasks = daemon.max_concurrent_tasks ?? "-";
  const runtimes = daemon.runtimes || "-";
  daemonPill.textContent = `daemon 上限 ${maxTasks} · ${runtimes}`;
  daemonPill.className = "pill good";

  const agents = runtime.agents || [];
  if (!agents.length) {
    agentsEl.innerHTML = `<div class="empty">无智能体</div>`;
  } else {
    agentsEl.innerHTML = agents
      .map((agent) => {
        const running = Number(agent.running_task_count || 0);
        const max = agent.max_concurrent_tasks ?? "-";
        const active = running > 0 ? "good" : "";
        return `
          <div class="runtime-agent">
            <div class="runtime-agent-name">${agent.name || agent.id}</div>
            <div class="runtime-agent-meta">
              上限 ${max} · 跑 task <span class="pill ${active}">${running}</span>
            </div>
          </div>
        `;
      })
      .join("");
  }

  const cli = runtime.local_cursor_cli || {};
  cliEl.textContent = `本机 cursor-agent/agent 进程: ${cli.total ?? 0}（portfolio ${cli.portfolio ?? 0}，multica daemon ${cli.multica_daemon ?? 0}，飞书桥接 ${cli.feishu_claw ?? 0}，其他 ${cli.other ?? 0}）· Multica task 合计 ${runtime.working_agents_total_running ?? 0}`;
}

async function selectProject(project) {
  if (!project) return;
  state.selectedRepo = project.repo;
  state.selectedProject = project;
  document.getElementById("queue-title").textContent = `${project.id} 队列`;
  document.getElementById("dispatch-one").disabled = !project.local_path;
  const hint = document.getElementById("path-hint");
  if (hint) {
    hint.textContent = project.local_path
      ? `本地路径: ${project.local_path}`
      : "未找到本地 checkout — 在 .ai-company/config/local.env 配置 AI_REPO_PATH_* 或 MUSIC_SAAS_PATH";
  }
  await loadQueue();
}

async function loadQueue() {
  if (!state.selectedRepo) return;
  const panel = document.getElementById("queue-panel");
  panel.innerHTML = `<div class="empty">加载中…</div>`;
  const data = await api(`/api/queue?repo=${encodeURIComponent(state.selectedRepo)}`);
  const sections = [
    ["可派单", data.queue, true],
    ["运行中", data.running, false],
    ["阻塞", data.blocked, false],
  ];

  panel.innerHTML = "";
  for (const [title, items, canDispatch] of sections) {
    const block = document.createElement("div");
    block.innerHTML = `<h3>${title} (${items.length})</h3>`;
    if (items.length === 0) {
      block.innerHTML += `<div class="empty">无</div>`;
    } else {
      const list = document.createElement("ul");
      list.className = "issue-list";
      for (const issue of items) {
        const li = document.createElement("li");
        li.className = "issue-item";
        li.innerHTML = `
          <div class="issue-title">#${issue.number} ${issue.title}</div>
          <div class="issue-meta">${issue.url}</div>
          <div class="issue-actions">
            <a href="${issue.url}" target="_blank" rel="noreferrer">打开 Issue</a>
            ${
              canDispatch
                ? `<button class="dispatch-issue" data-issue="${issue.number}">派这一单</button>`
                : ""
            }
          </div>
        `;
        list.appendChild(li);
      }
      block.appendChild(list);
    }
    panel.appendChild(block);
  }

  document.querySelectorAll(".dispatch-issue").forEach((button) => {
    button.addEventListener("click", async () => {
      const issue = button.getAttribute("data-issue");
      await dispatchIssue(issue);
    });
  });
}

async function loadOverview(refreshVerify = false) {
  const q = refreshVerify ? "?refresh_verify=1" : "";
  state.overview = await api(`/api/company-overview${q}`);
  state.projects = state.overview.projects || [];
  state.totals = state.overview.totals || {};
  renderOverview();
  formatTotals();
  renderProjects();
}

async function loadProjects() {
  if (state.overview) {
    state.projects = state.overview.projects || [];
    state.totals = state.overview.totals || {};
    formatTotals();
    renderProjects();
    return;
  }
  const data = await api("/api/projects");
  state.projects = data.projects;
  state.totals = data.totals;
  formatTotals();
  renderProjects();
}

async function loadMeta() {
  state.meta = await api("/api/meta");
  renderMeta();
}

async function loadJobs() {
  const data = await api("/api/jobs");
  state.jobs = data.jobs;
  const container = document.getElementById("jobs-panel");
  if (!state.jobs.length) {
    container.innerHTML = `<div class="empty">暂无派单任务</div>`;
    return;
  }

  container.innerHTML = state.jobs
    .map(
      (job) => `
      <div class="job-item" data-job="${job.id}">
        <div><strong>${job.mode}</strong> ${job.intake ? job.intake.slice(0, 40) : job.repo || "portfolio"} ${job.issue ? `#${job.issue}` : ""}</div>
        <div class="job-status">${job.status} · ${job.started_at || ""}</div>
        <button class="view-log" data-job="${job.id}">看日志</button>
      </div>
    `
    )
    .join("");

  document.querySelectorAll(".view-log").forEach((button) => {
    button.addEventListener("click", async () => {
      const jobId = button.getAttribute("data-job");
      const job = await api(`/api/jobs/${jobId}`);
      document.getElementById("log-box").textContent = job.log_tail || "(empty)";
    });
  });
}

async function dispatchPortfolio(maxTotal) {
  const job = await api("/api/dispatch", {
    method: "POST",
    body: JSON.stringify({ mode: "portfolio", max_total: maxTotal }),
  });
  await refreshAll();
  document.getElementById("log-box").textContent = `Started job ${job.id}\n${job.log_path || ""}`;
}

async function launchSiteFactory() {
  const intake = document.getElementById("site-factory-intake").value.trim();
  if (!intake) {
    alert("请输入建站想法，例如：做一个 JSON 格式化网站");
    return;
  }
  const createRepo = document.getElementById("site-factory-create-repo").checked;
  const activateAutopilot = createRepo
    ? document.getElementById("site-factory-activate-autopilot").checked
    : false;
  const job = await api("/api/site-factory", {
    method: "POST",
    body: JSON.stringify({
      intake,
      create_repo: createRepo,
      activate_autopilot: activateAutopilot,
      notify: true,
      max_dispatch: 2,
    }),
  });
  await refreshAll();
  document.getElementById("log-box").textContent = `Site factory job ${job.id}\n${job.log_path || ""}`;
}

async function dispatchIssue(issue) {
  const project = state.selectedProject;
  if (!project?.local_path) {
    alert("未找到本地 checkout。在 .ai-company/config/local.env 设置 AI_REPO_PATH_* 或 MUSIC_SAAS_PATH，或把仓库放在 ~/Projects / ~/Desktop 下让系统自动发现。");
    return;
  }
  const job = await api("/api/dispatch", {
    method: "POST",
    body: JSON.stringify({
      mode: "issue",
      repo: project.repo,
      issue,
      local_path: project.local_path,
    }),
  });
  await refreshAll();
  document.getElementById("log-box").textContent = `Started job ${job.id}\n${job.log_path || ""}`;
}

async function loadRuntime() {
  state.runtime = await api("/api/multica-runtime");
  renderRuntime();
}

async function refreshAll() {
  await Promise.all([loadMeta(), loadRuntime(), loadOverview(false), loadJobs()]);
  if (state.selectedRepo) await loadQueue();
}

function bindEvents() {
  document.getElementById("refresh-btn").addEventListener("click", refreshAll);
  document.getElementById("verify-refresh-btn").addEventListener("click", async () => {
    document.getElementById("verify-refresh-btn").disabled = true;
    try {
      await loadOverview(true);
    } finally {
      document.getElementById("verify-refresh-btn").disabled = false;
    }
  });
  document.getElementById("dispatch-portfolio").addEventListener("click", async () => {
    const maxTotal = Number(document.getElementById("max-total").value || "1");
    await dispatchPortfolio(maxTotal);
  });
  document.getElementById("site-factory-submit").addEventListener("click", async () => {
    await launchSiteFactory();
  });
  const createRepoEl = document.getElementById("site-factory-create-repo");
  const autopilotWrap = document.getElementById("site-factory-autopilot-wrap");
  const syncAutopilotVisibility = () => {
    autopilotWrap.style.display = createRepoEl.checked ? "inline-flex" : "none";
  };
  createRepoEl.addEventListener("change", syncAutopilotVisibility);
  syncAutopilotVisibility();
  document.getElementById("dispatch-one").addEventListener("click", async () => {
    const data = await api(`/api/queue?repo=${encodeURIComponent(state.selectedRepo)}`);
    if (!data.queue?.length) {
      alert("当前没有可派 issue。");
      return;
    }
    await dispatchIssue(String(data.queue[0].number));
  });
}

async function main() {
  bindEvents();
  await refreshAll();
  state.pollTimer = window.setInterval(() => {
    loadJobs();
    loadRuntime();
  }, 5000);
}

main().catch((error) => {
  document.body.innerHTML = `<pre style="padding:24px;color:#ff6b6b;">${error.message}</pre>`;
});

// A disposable process keeps synchronous extraction off the HTTP event loop.
const { CodeGraph } = require("/opt/codegraph/dist/index.js");
const fs = require("node:fs");
const path = require("node:path");
const { copySnapshot } = require("./copy-snapshot.cjs");
const privatePath = require("./snapshot-policy.cjs")(
  require("./snapshot-policy.json"),
);
const engineCommit = fs
  .readFileSync(path.join(__dirname, "source-revision.txt"), "utf8")
  .trim();
if (!/^[0-9a-f]{40}$/.test(engineCommit))
  throw Error("Invalid CodeGraph source revision");
const safeNode = (n) => ({
  id: n.id,
  kind: n.kind,
  name: n.name,
  qualified_name: n.qualifiedName,
  file_path: n.filePath,
  start_line: n.startLine,
  end_line: n.endLine,
});
async function run(input) {
  const parts = [input.workspace_id, input.repository_id, input.build_id];
  if (!parts.every((s) => typeof s === "string" && /^[0-9a-f-]{36}$/.test(s)))
    throw Error("Invalid snapshot");
  const root = process.env.MULTICA_CODEGRAPH_ROOT || "/data/codegraphs";
  const workRoot =
    process.env.MULTICA_CODEGRAPH_WORK_ROOT ||
    path.join(require("node:os").tmpdir(), "multica-codegraph-builds");
  const relative = path.join(parts[0], parts[1], "builds", parts[2]);
  const work = path.join(workRoot, relative);
  const destination = path.join(root, relative);
  const staging = destination + ".publishing";
  if (path.resolve(workRoot) === path.resolve(root))
    throw Error("Build storage must differ from persistent storage");
  if (input.op === "cleanup") {
    fs.rmSync(work, { recursive: true, force: true });
    fs.rmSync(staging, { recursive: true, force: true });
    return { engine_commit: engineCommit, data: { cleaned: true } };
  }
  if (input.op === "publish") {
    if (fs.existsSync(destination)) throw Error("Snapshot already exists");
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    const copyStarted = performance.now();
    const copy = await copySnapshot(work, staging);
    const copyMs = Math.round(performance.now() - copyStarted);
    const validationStarted = performance.now();
    let copied;
    try {
      copied = await CodeGraph.open(path.join(staging, "repo"), {
        readOnly: true,
      });
      const docs = JSON.parse(
        fs.readFileSync(path.join(staging, "documents.json"), "utf8"),
      );
      if (
        docs.version !== 1 ||
        docs.build_id !== input.build_id ||
        !Array.isArray(docs.nodes)
      )
        throw Error("Invalid document index");
      if (copied.getStats().nodeCount === 0 && docs.nodes.length === 0)
        throw Error("Empty published graph");
    } finally {
      copied?.close();
    }
    fs.renameSync(staging, destination);
    return {
      engine_commit: engineCommit,
      data: {
        published: true,
        copy: { ...copy, duration_ms: copyMs },
        validation_ms: Math.round(performance.now() - validationStarted),
      },
    };
  }
  const dir = path.join(
    ["prepare", "build"].includes(input.op) ? work : destination,
    "repo",
  );
  if (!fs.statSync(dir).isDirectory()) throw Error("Snapshot missing");
  if (input.op === "prepare") {
    const clean = (directory) => {
      for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
        const target = path.join(directory, entry.name);
        if (
          entry.isSymbolicLink() ||
          privatePath(
            path.relative(dir, target).split(path.sep).join("/"),
            entry.isDirectory(),
          )
        )
          fs.rmSync(target, { recursive: true, force: true });
        else if (entry.isDirectory()) clean(target);
      }
    };
    clean(dir);
    return { engine_commit: engineCommit, data: { prepared: true } };
  }
  let graph;
  try {
    graph =
      input.op === "build"
        ? await CodeGraph.init(dir, { index: false })
        : await CodeGraph.open(dir, { readOnly: true });
    if (input.op === "build") await graph.indexAll();
    let data,
      truncated = false;
    switch (input.op) {
      case "build":
      case "info": {
        const s = graph.getStats(),
          files = new Set(graph.getFiles().map((f) => f.path));
        try {
          const docs = JSON.parse(
            fs.readFileSync(
              path.join(path.dirname(dir), "documents.json"),
              "utf8",
            ),
          );
          if (
            docs.version !== 1 ||
            docs.build_id !== input.build_id ||
            !Array.isArray(docs.nodes)
          )
            throw Error("Invalid document index");
          for (const node of docs.nodes)
            if (node.kind === "document") files.add(node.file_path);
        } catch (error) {
          if (input.op === "build") throw error;
        }
        data = {
          files: files.size,
          code_files: s.fileCount,
          symbols: s.nodeCount,
          edges: s.edgeCount,
        };
        break;
      }
      case "search": {
        const q = String(input.q || "").slice(0, 200),
          symbolQuery = q.replaceAll("::", "."),
          normalized = symbolQuery.toLowerCase();
        const rank = (r) => {
          const qualified = String(r.node.qualifiedName || "")
            .replaceAll("::", ".")
            .toLowerCase();
          return qualified === normalized
            ? 3
            : qualified.endsWith("." + normalized)
              ? 2
              : String(r.node.name || "").toLowerCase() === normalized
                ? 1
                : 0;
        };
        const candidates = graph.searchNodes(q, { limit: 50 });
        if (/^[\w$]+(?:\.[\w$]+)+$/.test(symbolQuery)) {
          const known = new Set(candidates.map((r) => r.node.id));
          for (const node of graph.getNodesByName(
            symbolQuery.split(".").pop(),
          )) {
            if (!known.has(node.id) && rank({ node }) >= 2) {
              candidates.push({ node, score: 0 });
              known.add(node.id);
            }
          }
        }
        const rows = candidates.sort(
          (a, b) => rank(b) - rank(a) || b.score - a.score,
        );
        truncated = rows.length > (input.limit || 50) || rows.length >= 50;
        data = rows
          .slice(0, input.limit || 50)
          .map((r) => ({
            ...safeNode(r.node),
            score: r.score,
            match_type:
              rank(r) === 3
                ? "exact_qualified"
                : rank(r) === 2
                  ? "qualified_suffix"
                  : "lexical",
          }));
        break;
      }
      case "callers":
      case "callees": {
        const rows =
          input.op === "callers"
            ? graph.getCallers(input.symbol_id, 1)
            : graph.getCallees(input.symbol_id, 1);
        const limit = input.limit || 100;
        data = {
          items: rows
            .slice(0, limit)
            .map((r) => ({
              node: safeNode(r.node),
              kind: r.edge.kind,
              call_site_line: r.edge.line,
              provenance: r.edge.provenance,
            })),
          truncated: rows.length > limit,
        };
        break;
      }
      case "impact": {
        const sub = graph.getImpactRadius(input.symbol_id, 2);
        const limit = input.limit || 100,
          edgeLimit = input.limit ? limit * 2 : 200;
        data = {
          nodes: [...sub.nodes.values()].slice(0, limit).map(safeNode),
          edges: sub.edges
            .slice(0, edgeLimit)
            .map((e) => ({ source: e.source, target: e.target, kind: e.kind })),
          truncated: sub.nodes.size > limit || sub.edges.length > edgeLimit,
        };
        break;
      }
      default:
        throw Error("Unsupported operation");
    }
    return { engine_commit: engineCommit, data, truncated };
  } finally {
    graph?.close();
  }
}
let raw = "";
process.stdin.on("data", (b) => (raw += b));
process.stdin.on("end", () =>
  run(JSON.parse(raw))
    .then((result) =>
      process.stdout.write("\nMULTICA_RESULT:" + JSON.stringify(result) + "\n"),
    )
    .catch(() => {
      process.stderr.write("CodeGraph operation failed\n");
      process.exitCode = 1;
    }),
);

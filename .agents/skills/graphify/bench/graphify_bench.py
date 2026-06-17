#!/usr/bin/env python3
"""
graphify_bench.py — measure Graphify's token cost vs. naive file-reading.

For each (corpus, question) it:
  1. builds a CODE-ONLY knowledge graph offline (no API key, tree-sitter AST),
  2. runs `graphify query` (budget-capped) and counts the answer's tokens,
  3. sums the tokens of the source files the answer cites  (= "naive: read the
     relevant files", the realistic baseline an agent pays without the graph),
  4. sums the tokens of the whole corpus (= "naive: load everything", worst case),
  5. reports the ratios.

Token counts use tiktoken cl100k_base as a portable proxy. Absolute counts
differ slightly per model tokenizer; the RATIO is what matters and is robust.

Usage:
  uv run --with tiktoken python3 graphify_bench.py --config questions.json \
      --out report --budget 2000

Requires: `graphify` on PATH (uv tool install graphifyy), tiktoken.
"""
import argparse, json, os, re, shutil, subprocess, sys, tempfile

CODE_EXT = {
    ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".go", ".py", ".rs", ".java",
    ".rb", ".php", ".c", ".h", ".cpp", ".hpp", ".cs", ".kt", ".scala", ".swift",
    ".lua", ".zig", ".sql", ".json", ".sh", ".ps1", ".ex", ".exs", ".jl",
    ".vue", ".svelte", ".astro", ".dart", ".groovy",
}
SKIP_DIRS = {"node_modules", ".next", "dist", "build", ".git", "out", "coverage",
             ".turbo", "graphify-out", "__pycache__", ".venv"}

def enc_factory():
    import tiktoken
    enc = tiktoken.get_encoding("cl100k_base")
    return lambda s: len(enc.encode(s))

def file_tokens(path, toks):
    try:
        with open(path, encoding="utf-8", errors="ignore") as f:
            return toks(f.read())
    except Exception:
        return 0

def copy_code_only(src, dst):
    n = 0
    for root, dirs, files in os.walk(src):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        for fn in files:
            if os.path.splitext(fn)[1].lower() in CODE_EXT:
                sp = os.path.join(root, fn)
                rel = os.path.relpath(sp, src)
                dp = os.path.join(dst, rel)
                os.makedirs(os.path.dirname(dp), exist_ok=True)
                shutil.copy2(sp, dp)
                n += 1
    return n

def build_graph(code_dir, out_dir):
    r = subprocess.run(
        ["graphify", "extract", code_dir, "--no-cluster", "--out", out_dir],
        capture_output=True, text=True, timeout=900)
    gj = os.path.join(out_dir, "graphify-out", "graph.json")
    if not os.path.exists(gj):
        raise RuntimeError("graph build failed:\n" + r.stdout + r.stderr)
    return gj

def run_query(graph_json, question, budget):
    r = subprocess.run(
        ["graphify", "query", question, "--graph", graph_json, "--budget", str(budget)],
        capture_output=True, text=True, timeout=300)
    return r.stdout

def cited_src_files(query_out):
    files = set()
    for m in re.finditer(r"src=(\S+)", query_out):
        p = m.group(1)
        if p and p != "":
            files.add(p)
    return files

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--config", required=True)
    ap.add_argument("--out", default="report")
    ap.add_argument("--budget", type=int, default=2000)
    args = ap.parse_args()

    toks = enc_factory()
    cfg = json.load(open(args.config))
    os.makedirs(args.out, exist_ok=True)
    results = []

    for corpus in cfg["corpora"]:
        name, path = corpus["name"], corpus["path"]
        print(f"\n=== {name}  ({path}) ===", flush=True)
        tmp = tempfile.mkdtemp(prefix="gbench_src_")
        outd = tempfile.mkdtemp(prefix="gbench_out_")
        nfiles = copy_code_only(path, tmp)
        subsystem_tok = sum(
            file_tokens(os.path.join(r, f), toks)
            for r, _, fs in os.walk(tmp) for f in fs)
        gj = build_graph(tmp, outd)
        print(f"  {nfiles} code files, {subsystem_tok} subsystem tokens", flush=True)
        for q in corpus["questions"]:
            out = run_query(gj, q, args.budget)
            qtok = toks(out)
            cf = cited_src_files(out)
            naive_rel = sum(file_tokens(p, toks) for p in cf)
            row = {
                "corpus": name, "files": nfiles, "question": q,
                "graphify_tokens": qtok, "cited_files": len(cf),
                "naive_relevant_tokens": naive_rel,
                "naive_subsystem_tokens": subsystem_tok,
                "ratio_relevant": round(naive_rel / max(qtok, 1), 1),
                "ratio_subsystem": round(subsystem_tok / max(qtok, 1), 1),
            }
            results.append(row)
            print(f"  Q: {q[:60]}", flush=True)
            print(f"     graphify={qtok}  naive(relevant {len(cf)} files)={naive_rel} "
                  f"({row['ratio_relevant']}x)  naive(all)={subsystem_tok} "
                  f"({row['ratio_subsystem']}x)", flush=True)
        shutil.rmtree(tmp, ignore_errors=True)
        shutil.rmtree(outd, ignore_errors=True)

    json.dump(results, open(os.path.join(args.out, "results.json"), "w"), indent=2)
    write_markdown(results, os.path.join(args.out, "report.md"), args.budget)
    print(f"\nWrote {args.out}/results.json and {args.out}/report.md")

def write_markdown(rows, path, budget):
    lines = ["# Graphify token-cost benchmark", ""]
    lines.append(f"Query budget: {budget} tokens. Token counter: tiktoken cl100k_base "
                 "(portable proxy; ratios are tokenizer-robust).")
    lines.append("")
    lines.append("**Naive (relevant files)** = tokens to read the source files the graph "
                 "answer cites — the realistic cost an agent pays to answer without the graph.")
    lines.append("**Naive (whole subsystem)** = tokens to load every code file in the path "
                 "(worst case).")
    lines.append("")
    lines.append("| Corpus | Question | Graphify | Naive (relevant) | Saving | Naive (all) |")
    lines.append("|---|---|---|---|---|---|")
    for r in rows:
        lines.append(f"| {r['corpus']} ({r['files']} files) | {r['question']} | "
                     f"{r['graphify_tokens']} | {r['naive_relevant_tokens']} "
                     f"({r['cited_files']} files) | {r['ratio_relevant']}x | "
                     f"{r['naive_subsystem_tokens']} ({r['ratio_subsystem']}x) |")
    if rows:
        rr = [r["ratio_relevant"] for r in rows]
        lines += ["", f"**Realistic saving range: {min(rr)}x – {max(rr)}x** "
                  f"across {len(rows)} questions.", ""]
    lines.append("> Caveat: this measures the cost of NAVIGATION / orientation "
                 "questions ('where is X', 'what connects to Y'). When an agent must "
                 "EDIT code it still reads the real files — so real-world savings depend "
                 "on the find-vs-edit mix of the workload.")
    open(path, "w").write("\n".join(lines) + "\n")

if __name__ == "__main__":
    main()

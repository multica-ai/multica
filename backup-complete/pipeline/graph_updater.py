#!/usr/bin/env python3
"""
Hermes Memory Graph Updater — تحديث الرسم المعرفي للحقائق

يقرأ الحقائق من ~/.hermes/memory/facts_auto/
يضيفها لـ ~/.hermes/graphs/hermes-memory/
باستخدام BGE-M3 (نفس نموذج التضمين في النظام الأساسي)

هذا يضمن أن prefetch() يجد الحقائق تلقائياً.

الموديلات:
- BGE-M3 (1024-dim) — نفس unified plugin
- Ollama qwen2.5:3b — للتلخيص (في session_summarizer)
"""

import json
import os
import hashlib
import time
import sys
import fcntl
from pathlib import Path
from datetime import datetime, timezone

# ============================================================
# Configuration
# ============================================================
FACTS_DIR = os.path.expanduser("~/.hermes/memory/facts_auto")
GRAPHS_DIR = os.path.expanduser("~/.hermes/graphs")
PROJECT_NAME = "hermes-memory"
TRACKER_FILE = os.path.expanduser("~/.hermes/memory/.graph_tracker.json")

EMBEDDING_DIM = 1024  # BGE-M3 dimension

# عتبة التشابه الدلالي للكشف عن التكرار (cosine similarity)
# 0.92 = الحقيقة الجديدة شبه مطابقة لحقيقة موجودة (إعادة صياغة)
# 0.95+ صارمة جداً، 0.85- متساهلة جداً وتدمج حقائق متمايزة
# تجاوز هذه العتبة → تحديث الموجود بدلاً من إضافة جديد
SEMANTIC_DEDUP_THRESHOLD = 0.92

VALID_CATEGORIES = (
    "preference", "fact", "decision", "correction",
    "project", "technical", "personal", "service", "general"
)

# ============================================================
# Tracker
# ============================================================

def load_graph_tracker():
    if os.path.exists(TRACKER_FILE):
        try:
            with open(TRACKER_FILE) as f:
                data = json.load(f)
            if isinstance(data.get("indexed_fact_hashes"), list):
                return data
        except (json.JSONDecodeError, KeyError):
            pass
    return {"indexed_fact_hashes": []}

def save_graph_tracker(tracker):
    tmp = TRACKER_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(tracker, f, indent=2)
    os.replace(tmp, TRACKER_FILE)

# ============================================================
# BGE-M3 Embedding (from unified plugin)
# ============================================================

_embedding_model = None

def get_embedding_model():
    """Load BGE-M3 singleton from unified plugin."""
    global _embedding_model
    if _embedding_model is not None:
        return _embedding_model

    # Add unified plugin to path
    plugin_dir = os.path.expanduser("~/.hermes/plugins")
    if plugin_dir not in sys.path:
        sys.path.insert(0, plugin_dir)

    from unified.embedding_model import EmbeddingModel
    _embedding_model = EmbeddingModel(
        model_name="BAAI/bge-m3",
        device="cpu",
        use_fp16=False,
    )
    return _embedding_model

def get_embedding(text):
    """Get BGE-M3 embedding (1024-dim)."""
    model = get_embedding_model()
    return model.embed_query(text)

# ============================================================
# Load/save graph
# ============================================================

def load_or_create_graph():
    import networkx as nx

    graph_path = os.path.join(GRAPHS_DIR, PROJECT_NAME, "graph.json")
    if os.path.exists(graph_path):
        with open(graph_path) as f:
            data = json.load(f)
        return nx.node_link_graph(data)
    return nx.Graph()

def save_graph(graph):
    import networkx as nx

    project_dir = os.path.join(GRAPHS_DIR, PROJECT_NAME)
    os.makedirs(project_dir, exist_ok=True)

    # Preserve created_at
    meta_path = os.path.join(project_dir, "metadata.json")
    existing_meta = {}
    if os.path.exists(meta_path):
        try:
            with open(meta_path) as f:
                existing_meta = json.load(f)
        except:
            pass

    # Save graph (compact)
    graph_path = os.path.join(project_dir, "graph.json")
    graph_data = nx.node_link_data(graph)
    with open(graph_path, "w", encoding="utf-8") as f:
        json.dump(graph_data, f, ensure_ascii=False, separators=(",", ":"))

    # Save metadata
    metadata = {
        "project_name": PROJECT_NAME,
        "created_at": existing_meta.get("created_at", datetime.now(timezone.utc).isoformat()),
        "updated_at": datetime.now(timezone.utc).isoformat(),
        "num_nodes": graph.number_of_nodes(),
        "num_edges": graph.number_of_edges(),
        "embedding_model": "BAAI/bge-m3",
        "embedding_dim": EMBEDDING_DIM,
    }
    with open(meta_path, "w", encoding="utf-8") as f:
        json.dump(metadata, f, ensure_ascii=False, separators=(",", ":"))

    # Save communities (by category)
    communities = {}
    for node, data in graph.nodes(data=True):
        cat = data.get("category", "general")
        communities.setdefault(cat, []).append(node)

    comm_path = os.path.join(project_dir, "communities.json")
    with open(comm_path, "w", encoding="utf-8") as f:
        json.dump({
            "communities": communities,
            "num_communities": len(communities),
        }, f, ensure_ascii=False, separators=(",", ":"))

    print(f"  Graph saved: {graph.number_of_nodes()} nodes, {graph.number_of_edges()} edges")

# ============================================================
# Read new facts
# ============================================================

def read_new_facts():
    tracker = load_graph_tracker()
    indexed = set(tracker.get("indexed_fact_hashes", []))
    new_facts = []

    if not os.path.exists(FACTS_DIR):
        os.makedirs(FACTS_DIR, exist_ok=True)
        return new_facts, indexed

    for filename in os.listdir(FACTS_DIR):
        if not filename.endswith(".jsonl"):
            continue

        file_category = filename.replace(".jsonl", "")
        filepath = os.path.join(FACTS_DIR, filename)

        with open(filepath, encoding="utf-8") as f:
            fcntl.flock(f.fileno(), fcntl.LOCK_SH)
            try:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        fact = json.loads(line)
                    except json.JSONDecodeError:
                        continue

                    if "category" not in fact:
                        fact["category"] = file_category

                    # Deduplicate by key text only (not full JSON with timestamp)
                    key = fact.get("key", "").strip()
                    if not key:
                        continue
                    fact_hash = hashlib.md5(key.encode()).hexdigest()

                    if fact_hash not in indexed:
                        new_facts.append(fact)
                        indexed.add(fact_hash)
            finally:
                fcntl.flock(f.fileno(), fcntl.LOCK_UN)

    return new_facts, indexed

# ============================================================
# Orphan node cleanup — remove fact nodes no longer in JSONL files
# ============================================================

def collect_all_fact_keys():
    """Collect all fact keys from JSONL files (the source of truth)."""
    all_keys = set()
    if not os.path.exists(FACTS_DIR):
        return all_keys

    for filename in os.listdir(FACTS_DIR):
        if not filename.endswith(".jsonl"):
            continue
        filepath = os.path.join(FACTS_DIR, filename)
        with open(filepath, encoding="utf-8") as f:
            fcntl.flock(f.fileno(), fcntl.LOCK_SH)
            try:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        fact = json.loads(line)
                        key = fact.get("key", "").strip()
                        if key:
                            all_keys.add(key)
                    except json.JSONDecodeError:
                        continue
            finally:
                fcntl.flock(f.fileno(), fcntl.LOCK_UN)

    return all_keys

def remove_orphan_nodes(graph):
    """Remove fact-type nodes whose content is no longer in any JSONL file.
    Returns (orphan_count, category_count, orphan_hashes) — hashes must be
    removed from the tracker so re-added facts aren't blocked."""
    all_keys = collect_all_fact_keys()
    if not all_keys:
        print("  No fact keys in JSONL files — skipping orphan cleanup")
        return 0, 0, set()

    orphan_nodes = []
    orphan_hashes = set()
    for node_id, data in graph.nodes(data=True):
        if data.get("type") != "fact":
            continue
        content = data.get("content", "")
        if content not in all_keys:
            # Check aliases too
            aliases = data.get("aliases", [])
            if any(alias in all_keys for alias in aliases):
                continue  # Still referenced
            orphan_nodes.append(node_id)
            # Compute hash for tracker cleanup
            orphan_hashes.add(hashlib.md5(content.encode()).hexdigest())

    for node_id in orphan_nodes:
        graph.remove_node(node_id)

    # Also remove orphan category/session nodes with no edges
    removed_cats = 0
    for node_id in list(graph.nodes()):
        data = graph.nodes[node_id]
        if data.get("type") in ("category", "session"):
            if graph.degree(node_id) == 0:
                graph.remove_node(node_id)
                removed_cats += 1

    print(f"  Orphan cleanup: {len(orphan_nodes)} fact nodes + {removed_cats} category/session nodes removed")
    return len(orphan_nodes), removed_cats, orphan_hashes

# ============================================================
# Add facts to graph
# ============================================================

def _node_id(text):
    return hashlib.md5(text.encode()).hexdigest()[:12]

def add_facts_to_graph(graph, facts):
    import numpy as np

    now = datetime.now(timezone.utc).isoformat()
    nodes_added = 0
    edges_added = 0
    nodes_merged = 0
    new_node_ids = []

    # ====================================================
    # Pre-build matrix of existing fact embeddings for dedup
    # ====================================================
    existing_fact_nodes = []  # list of (node_id, normalized_embedding)
    existing_fact_matrix = None
    
    for nid, ndata in graph.nodes(data=True):
        if ndata.get("type") != "fact":
            continue
        emb = ndata.get("embedding")
        if emb and len(emb) == EMBEDDING_DIM:
            existing_fact_nodes.append(nid)
    
    if existing_fact_nodes:
        raw_matrix = np.asarray(
            [graph.nodes[nid]["embedding"] for nid in existing_fact_nodes],
            dtype=np.float32,
        )
        norms = np.linalg.norm(raw_matrix, axis=1, keepdims=True)
        existing_fact_matrix = raw_matrix / np.maximum(norms, 1e-10)

    # Pre-generate category embeddings
    categories = set(f.get("category", "general") for f in facts)
    cat_embeddings = {}
    for cat in categories:
        cat_id = f"category_{cat}"
        if graph.has_node(cat_id):
            cat_embeddings[cat_id] = graph.nodes[cat_id].get("embedding", [])
        else:
            cat_embeddings[cat_id] = get_embedding(f"Category: {cat}")

    for fact in facts:
        key = fact.get("key", "")
        category = fact.get("category", "general")
        if category not in VALID_CATEGORIES:
            category = "general"
        session_id = fact.get("session_id", "")
        importance = fact.get("importance", 1)

        if not key:
            continue

        node_id = f"fact_{_node_id(key)}"

        if graph.has_node(node_id):
            # نفس الـ key بالحرف — تخطّي تماماً (السلوك القديم)
            target_node_id = node_id
        else:
            embedding = get_embedding(key)

            # ====================================================
            # Semantic dedup: ابحث عن fact موجود متشابه دلالياً
            # ====================================================
            duplicate_of = None
            if existing_fact_matrix is not None and len(existing_fact_matrix) > 0:
                q = np.asarray(embedding, dtype=np.float32)
                q = q / max(np.linalg.norm(q), 1e-10)
                sims = existing_fact_matrix @ q
                max_idx = int(np.argmax(sims))
                max_sim = float(sims[max_idx])
                if max_sim >= SEMANTIC_DEDUP_THRESHOLD:
                    duplicate_of = existing_fact_nodes[max_idx]

            if duplicate_of is not None:
                # دمج: عزّز الموجود بدلاً من إنشاء نود جديد
                existing = graph.nodes[duplicate_of]
                existing["importance"] = min(
                    5,
                    max(
                        existing.get("importance", 1),
                        importance,
                    )  # لا نزيد تلقائياً، فقط نضمن الأعلى
                )
                existing["seen_count"] = existing.get("seen_count", 1) + 1
                existing["last_seen_at"] = now
                # احفظ الصياغة البديلة (مفيد للسياق)
                aliases = existing.get("aliases", [])
                if key not in aliases and key != existing.get("content"):
                    aliases.append(key)
                    existing["aliases"] = aliases[:10]  # حد أقصى 10 صياغات
                target_node_id = duplicate_of
                nodes_merged += 1
            else:
                graph.add_node(
                    node_id,
                    content=key,
                    type="fact",
                    category=category,
                    session_id=session_id,
                    importance=importance,
                    seen_count=1,
                    created_at=now,
                    last_seen_at=now,
                    embedding=embedding,
                )
                nodes_added += 1
                new_node_ids.append(node_id)
                target_node_id = node_id

                # حدّث المصفوفة المُسرَّعة لكي يُكشَف التكرار داخل هذه الدفعة نفسها
                q_normalized = np.asarray(embedding, dtype=np.float32)
                q_normalized = q_normalized / max(np.linalg.norm(q_normalized), 1e-10)
                if existing_fact_matrix is None:
                    existing_fact_matrix = q_normalized.reshape(1, -1)
                else:
                    existing_fact_matrix = np.vstack([
                        existing_fact_matrix,
                        q_normalized.reshape(1, -1),
                    ])
                existing_fact_nodes.append(node_id)

                # Edge to category (لـ nodes جديدة فقط)
                cat_id = f"category_{category}"
                if not graph.has_node(cat_id):
                    graph.add_node(
                        cat_id,
                        content=f"Category: {category}",
                        type="category",
                        category=category,
                        created_at=now,
                        embedding=cat_embeddings.get(cat_id, [0.0] * EMBEDDING_DIM),
                    )
                if not graph.has_edge(node_id, cat_id):
                    graph.add_edge(node_id, cat_id, weight=0.9, type="belongs_to")
                    edges_added += 1

        # Edge to session — يُضاف للنود الهدف (جديد أو مدموج)
        if session_id:
            session_node = f"session_{_node_id(session_id)}"
            if not graph.has_node(session_node):
                graph.add_node(
                    session_node,
                    content=f"Session: {session_id[:16]}",
                    type="session",
                    session_id=session_id,
                    created_at=now,
                    embedding=get_embedding(f"Session {session_id[:16]}"),
                )
            if not graph.has_edge(target_node_id, session_node):
                graph.add_edge(target_node_id, session_node, weight=0.7, type="from_session")
                edges_added += 1

    # ====================================================
    # روابط التشابه بين النودات الجديدة والموجودة (للاسترجاع لاحقاً)
    # ملاحظة: عتبة 0.7 هنا للروابط فقط — أي pair فوق DEDUP_THRESHOLD
    # تم دمجها أصلاً في اللوب أعلاه ولن تصل إلى هنا.
    # ====================================================
    if new_node_ids:
        new_embs = []
        for nid in new_node_ids:
            emb = graph.nodes[nid].get("embedding", [])
            if len(emb) == EMBEDDING_DIM:
                new_embs.append((nid, emb))

        existing_facts = []
        for n, d in graph.nodes(data=True):
            if d.get("type") == "fact" and n not in set(new_node_ids):
                emb = d.get("embedding", [])
                if len(emb) == EMBEDDING_DIM:
                    existing_facts.append((n, emb))

        if new_embs and existing_facts:
            new_arr = np.array([e for _, e in new_embs])
            old_arr = np.array([e for _, e in existing_facts])

            new_arr = new_arr / np.maximum(np.linalg.norm(new_arr, axis=1, keepdims=True), 1e-10)
            old_arr = old_arr / np.maximum(np.linalg.norm(old_arr, axis=1, keepdims=True), 1e-10)

            sim = new_arr @ old_arr.T

            for i in range(len(new_embs)):
                for j in range(len(existing_facts)):
                    # النطاق: 0.7 ≤ sim < SEMANTIC_DEDUP_THRESHOLD
                    # (أعلى من ذلك تم دمجه، أقل لا يستحق رابطاً)
                    if 0.7 < sim[i][j] < SEMANTIC_DEDUP_THRESHOLD:
                        ni, nj = new_embs[i][0], existing_facts[j][0]
                        if not graph.has_edge(ni, nj):
                            graph.add_edge(ni, nj, weight=float(sim[i][j]), type="similar")
                            edges_added += 1

        # new vs new
        if len(new_embs) > 1:
            arr = np.array([e for _, e in new_embs])
            arr = arr / np.maximum(np.linalg.norm(arr, axis=1, keepdims=True), 1e-10)
            sim = arr @ arr.T
            for i in range(len(new_embs)):
                for j in range(i + 1, len(new_embs)):
                    if 0.7 < sim[i][j] < SEMANTIC_DEDUP_THRESHOLD:
                        ni, nj = new_embs[i][0], new_embs[j][0]
                        if not graph.has_edge(ni, nj):
                            graph.add_edge(ni, nj, weight=float(sim[i][j]), type="similar")
                            edges_added += 1

    return nodes_added, edges_added, nodes_merged

# ============================================================
# Main
# ============================================================

def main():
    print("=" * 60)
    print(f"Hermes Memory Graph Updater (BGE-M3 — same as unified)")
    print("=" * 60)

    # Load BGE-M3
    print("  Loading BGE-M3 embedding model...")
    start_load = time.time()
    try:
        model = get_embedding_model()
        print(f"  Model loaded in {time.time() - start_load:.1f}s")
    except Exception as e:
        print(f"  ERROR: Failed to load BGE-M3: {e}")
        return

    graph = load_or_create_graph()
    print(f"  Existing graph: {graph.number_of_nodes()} nodes, {graph.number_of_edges()} edges")

    # Orphan cleanup BEFORE adding new facts
    orphan_facts, orphan_others, orphan_hashes = remove_orphan_nodes(graph)

    result = read_new_facts()
    new_facts, indexed_hashes = result

    # Clean orphan hashes from tracker so re-added facts aren't blocked
    if orphan_hashes:
        indexed_hashes -= orphan_hashes

    if not new_facts:
        print("  No new facts to add.")
        return

    print(f"  New facts found: {len(new_facts)}")

    start = time.time()
    nodes_added, edges_added, nodes_merged = add_facts_to_graph(graph, new_facts)
    elapsed = time.time() - start

    print(f"  Nodes added: {nodes_added}")
    print(f"  Nodes merged (semantic dedup): {nodes_merged}")
    print(f"  Edges added: {edges_added}")
    print(f"  Embedding time: {elapsed:.1f}s")

    save_graph(graph)

    tracker = load_graph_tracker()
    tracker["indexed_fact_hashes"] = list(indexed_hashes)
    save_graph_tracker(tracker)

    print(f"\n✓ Graph updated: {graph.number_of_nodes()} nodes, {graph.number_of_edges()} edges")
    print(f"  → This graph is now searchable via prefetch() automatically!")

if __name__ == "__main__":
    main()

package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const documentIndexVersion = 1
const maxDocumentNodes = 50000
const maxDocumentBytes = 16 << 20

type DocumentNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	ValueType string `json:"value_type,omitempty"`
}

type DocumentEdge struct {
	Source    string `json:"source"`
	Target    string `json:"target,omitempty"`
	Kind      string `json:"kind"`
	Reference string `json:"reference,omitempty"`
	Resolved  bool   `json:"resolved"`
}

type DocumentCoverage struct {
	Files      int      `json:"files"`
	Skipped    int      `json:"skipped"`
	Incomplete bool     `json:"incomplete"`
	Warnings   []string `json:"warnings"`
}

type DocumentIndex struct {
	Version   int              `json:"version"`
	BuildID   string           `json:"build_id"`
	Commit    string           `json:"commit"`
	Coverage  DocumentCoverage `json:"coverage"`
	Nodes     []DocumentNode   `json:"nodes"`
	Edges     []DocumentEdge   `json:"edges"`
	nodeLimit bool
	nodeIDs   map[string]bool
}

func documentID(file, kind, name string, line int) string {
	data, _ := json.Marshal([]any{file, kind, name, line})
	sum := sha256.Sum256(data)
	return "doc:" + hex.EncodeToString(sum[:12])
}

func (d *DocumentIndex) add(file, kind, name, typ string, start, end int) string {
	id := documentID(file, kind, name, start)
	if d.nodeIDs == nil {
		d.nodeIDs = map[string]bool{}
	}
	if d.nodeIDs[id] {
		return id
	}
	if len(d.Nodes) >= maxDocumentNodes {
		d.nodeLimit = true
		return ""
	}
	if end < start {
		end = start
	}
	d.nodeIDs[id] = true
	d.Nodes = append(d.Nodes, DocumentNode{id, kind, name, file, start, end, typ})
	return id
}

func (d *DocumentIndex) edge(source, target, kind, reference string) {
	if source == "" || len(d.Edges) >= maxDocumentNodes*2 {
		d.Coverage.Incomplete = true
		return
	}
	d.Edges = append(d.Edges, DocumentEdge{source, target, kind, reference, target != ""})
}

// IndexDocuments never executes includes, expands variables, or follows symbolic links.
func IndexDocuments(ctx context.Context, dir, build, commit string) (*DocumentIndex, error) {
	d := &DocumentIndex{Version: documentIndexVersion, BuildID: build, Commit: commit, Nodes: []DocumentNode{}, Edges: []DocumentEdge{}, Coverage: DocumentCoverage{Warnings: []string{}}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := make(map[string]string)
	scanned := 0
	err = fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return errors.New("Cannot scan document snapshot")
		}
		if entry.IsDir() {
			if PrivatePath(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if PrivatePath(rel, false) || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		scanned++
		if scanned > 100000 {
			return errors.New("Document snapshot exceeds file limit")
		}
		ext := strings.ToLower(path.Ext(rel))
		if ext != ".md" && ext != ".markdown" && ext != ".yaml" && ext != ".yml" {
			return nil
		}
		beforeNodes, beforeEdges := len(d.Nodes), len(d.Edges)
		data, readErr := readText(root, rel)
		if readErr == nil {
			id := d.add(rel, "document", rel, "", 1, strings.Count(string(data), "\n")+1)
			files[rel] = id
			if ext == ".md" || ext == ".markdown" {
				readErr = d.markdown(rel, data, id)
			} else {
				readErr = d.yaml(rel, data, id)
			}
		}
		if readErr != nil || d.nodeLimit {
			d.Nodes = d.Nodes[:beforeNodes]
			d.Edges = d.Edges[:beforeEdges]
			delete(files, rel)
			d.Coverage.Skipped++
			d.Coverage.Incomplete = true
			if len(d.Coverage.Warnings) < 50 {
				d.Coverage.Warnings = append(d.Coverage.Warnings, "Skipped document: "+rel)
			}
			if d.nodeLimit {
				return errors.New("Document snapshot exceeds node limit")
			}
		} else {
			d.Coverage.Files++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range d.Edges {
		e := &d.Edges[i]
		if e.Reference == "" || e.Kind == "environment" {
			continue
		}
		if target := files[e.Reference]; target != "" {
			e.Target = target
			e.Resolved = true
			continue
		}
		// Code references are resolved only to safe files present in this snapshot.
		if _, err := readText(root, e.Reference); err == nil {
			e.Target = "file:" + e.Reference
			e.Resolved = true
		}
	}
	return d, nil
}

func (d *DocumentIndex) Save(file string) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(data) > maxDocumentBytes {
		return errors.New("Document index exceeds size limit")
	}
	return os.WriteFile(file, data, 0600)
}

func LoadDocuments(file, build, commit string) (*DocumentIndex, error) {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
		return nil, errors.New("Document index unavailable; rebuild the graph")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, errors.New("Document index unavailable; rebuild the graph")
	}
	var d DocumentIndex
	if json.Unmarshal(data, &d) != nil || d.Version != documentIndexVersion || d.BuildID != build || d.Commit != commit || len(d.Nodes) > maxDocumentNodes || len(d.Edges) > maxDocumentNodes*2 {
		return nil, errors.New("Document index invalid; rebuild the graph")
	}
	ids := map[string]bool{}
	for _, node := range d.Nodes {
		if PrivatePath(node.FilePath, false) || node.StartLine < 1 || node.EndLine < node.StartLine || !strings.HasPrefix(node.ID, "doc:") || ids[node.ID] {
			return nil, errors.New("Document index invalid; rebuild the graph")
		}
		ids[node.ID] = true
	}
	for _, edge := range d.Edges {
		if !ids[edge.Source] || (edge.Resolved && edge.Target == "") {
			return nil, errors.New("Document index invalid; rebuild the graph")
		}
		if strings.HasPrefix(edge.Target, "doc:") && !ids[edge.Target] {
			return nil, errors.New("Document index invalid; rebuild the graph")
		}
		if edge.Kind != "environment" && edge.Reference != "" && PrivatePath(edge.Reference, false) {
			return nil, errors.New("Document index invalid; rebuild the graph")
		}
	}
	return &d, nil
}

func (d *DocumentIndex) Search(query string) map[string]any {
	q := strings.ToLower(query)
	nodes := []DocumentNode{}
	truncated := false
	for _, node := range d.Nodes {
		if q != "" && !strings.Contains(strings.ToLower(node.Name+" "+node.FilePath+" "+node.Kind), q) {
			continue
		}
		if len(nodes) == 50 {
			truncated = true
			break
		}
		nodes = append(nodes, node)
	}
	return map[string]any{"nodes": nodes, "truncated": truncated, "coverage": d.Coverage}
}

func (d *DocumentIndex) References(id string) (map[string]any, error) {
	var selected *DocumentNode
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			selected = &d.Nodes[i]
			break
		}
	}
	if selected == nil {
		return nil, errors.New("Document node unavailable")
	}
	edges := []DocumentEdge{}
	truncated := false
	for _, edge := range d.Edges {
		if edge.Source == id || edge.Target == id {
			if len(edges) == 100 {
				truncated = true
				break
			}
			edges = append(edges, edge)
		}
	}
	return map[string]any{"node": selected, "edges": edges, "truncated": truncated}, nil
}

func localDocumentReference(file, raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "?\\\x00") || strings.Contains(raw, ":") || strings.HasPrefix(raw, "/") {
		return ""
	}
	raw = strings.SplitN(raw, "#", 2)[0]
	if raw == "" {
		return ""
	}
	resolved := path.Clean(path.Join(path.Dir(file), raw))
	if PrivatePath(resolved, false) {
		return ""
	}
	return resolved
}

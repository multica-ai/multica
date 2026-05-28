// Package daggraph provides graph analysis over DAG link projections.
// It implements cycle detection, topological sort, and critical path
// without external dependencies, matching the "robot-mode" deterministic
// output style of beads_viewer.
package daggraph

import (
	"fmt"
	"sort"
)

// Node represents a record in the dependency graph.
type Node struct {
	ID   string
	Type string
}

// Edge represents a directed link between nodes.
type Edge struct {
	From string
	To   string
	Type string
}

// Graph is an adjacency-list representation of the dependency graph.
type Graph struct {
	nodes map[string]Node
	edges map[string][]Edge
}

// NewGraph builds a graph from a slice of edges.
func NewGraph(edges []Edge) *Graph {
	return NewGraphWithNodes(edges, nil)
}

// NewGraphWithNodes builds a graph from edges and explicit nodes.
// Nodes without edges are still included in the graph.
func NewGraphWithNodes(edges []Edge, extraNodes []Node) *Graph {
	g := &Graph{
		nodes: make(map[string]Node),
		edges: make(map[string][]Edge),
	}
	for _, e := range edges {
		g.edges[e.From] = append(g.edges[e.From], e)
		g.nodes[e.From] = Node{ID: e.From}
		g.nodes[e.To] = Node{ID: e.To}
	}
	for _, n := range extraNodes {
		if _, ok := g.nodes[n.ID]; !ok {
			g.nodes[n.ID] = n
		}
	}
	return g
}

// Cycles detects all elementary cycles using DFS from each node.
// Returns cycles as slices of node IDs. Empty slice means acyclic.
func (g *Graph) Cycles() [][]string {
	var cycles [][]string
	visited := make(map[string]bool)

	var dfs func(path []string, current string)
	dfs = func(path []string, current string) {
		for _, e := range g.edges[current] {
			next := e.To
			// Check if next is already in path (cycle found)
			for i, node := range path {
				if node == next {
					cycle := make([]string, len(path[i:])+1)
					copy(cycle, path[i:])
					cycle[len(cycle)-1] = next
					cycles = append(cycles, cycle)
					break
				}
			}
			if !visited[next] {
				visited[next] = true
				dfs(append(path, next), next)
				visited[next] = false
			}
		}
	}

	nodeIDs := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	for _, start := range nodeIDs {
		visited = make(map[string]bool)
		visited[start] = true
		dfs([]string{start}, start)
	}

	// Deduplicate cycles (same cycle can be found from different starting points)
	seen := make(map[string]bool)
	var unique [][]string
	for _, c := range cycles {
		// Normalize cycle: rotate to start with lexicographically smallest node
		if len(c) == 0 {
			continue
		}
		minIdx := 0
		for i := 1; i < len(c)-1; i++ {
			if c[i] < c[minIdx] {
				minIdx = i
			}
		}
		normalized := make([]string, len(c)-1)
		for i := 0; i < len(normalized); i++ {
			normalized[i] = c[(minIdx+i)%(len(c)-1)]
		}
		key := ""
		for _, n := range normalized {
			key += n + ","
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, c)
		}
	}

	// Sort for determinism
	sort.Slice(unique, func(i, j int) bool {
		if len(unique[i]) != len(unique[j]) {
			return len(unique[i]) < len(unique[j])
		}
		for k := 0; k < len(unique[i]) && k < len(unique[j]); k++ {
			if unique[i][k] != unique[j][k] {
				return unique[i][k] < unique[j][k]
			}
		}
		return len(unique[i]) < len(unique[j])
	})

	return unique
}

// TopologicalSort returns a valid topological ordering, or an error if cycles exist.
func (g *Graph) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(g.nodes))
	for id := range g.nodes {
		inDegree[id] = 0
	}
	for _, edges := range g.edges {
		for _, e := range edges {
			inDegree[e.To]++
		}
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	result := make([]string, 0, len(g.nodes))
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		result = append(result, u)

		next := make([]string, 0)
		for _, e := range g.edges[u] {
			inDegree[e.To]--
			if inDegree[e.To] == 0 {
				next = append(next, e.To)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}

	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("graph contains cycles")
	}
	return result, nil
}

// CriticalPath computes the longest path length from each node to any leaf.
// Returns a map of node ID to path length (number of edges).
func (g *Graph) CriticalPath() map[string]int {
	order, err := g.TopologicalSort()
	if err != nil {
		return nil
	}

	// Reverse topological order for DP
	pathLen := make(map[string]int, len(g.nodes))
	for i := len(order) - 1; i >= 0; i-- {
		u := order[i]
		maxLen := 0
		for _, e := range g.edges[u] {
			if l := pathLen[e.To] + 1; l > maxLen {
				maxLen = l
			}
		}
		pathLen[u] = maxLen
	}
	return pathLen
}

// subgraph returns a new graph containing only edges where both endpoints are in allowed.
func (g *Graph) subgraph(allowed []string) *Graph {
	allowedSet := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = true
	}
	var edges []Edge
	for _, es := range g.edges {
		for _, e := range es {
			if allowedSet[e.From] && allowedSet[e.To] {
				edges = append(edges, e)
			}
		}
	}
	return NewGraph(edges)
}

// NodesCount returns the number of nodes in the graph.
func (g *Graph) NodesCount() int {
	return len(g.nodes)
}

// Nodes returns all node IDs in the graph.
func (g *Graph) Nodes() []string {
	ids := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// HasEdge reports whether there is an edge from u to v.
func (g *Graph) HasEdge(from, to string) bool {
	for _, e := range g.edges[from] {
		if e.To == to {
			return true
		}
	}
	return false
}

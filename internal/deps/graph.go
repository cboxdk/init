package deps

import (
	"fmt"

	"github.com/cboxdk/init/internal/config"
)

// Graph represents a directed acyclic graph (DAG) of process dependencies
type Graph struct {
	// nodes maps process name to its dependencies
	nodes map[string][]string
}

// NewGraph creates a new empty dependency graph
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string][]string),
	}
}

// NewGraphFromConfig creates a dependency graph from process configs
// This is a convenience constructor for easy integration with the process manager
func NewGraphFromConfig(processes map[string]*config.Process) (*Graph, error) {
	g := NewGraph()

	// Build nodes from enabled processes only
	for name, proc := range processes {
		if !proc.Enabled {
			continue
		}
		g.AddNode(name, proc.DependsOn)
	}

	// Validate the graph
	if err := g.Validate(); err != nil {
		return nil, err
	}

	return g, nil
}

// AddNode adds a process to the graph with its dependencies
func (g *Graph) AddNode(name string, deps []string) {
	g.nodes[name] = deps
}

// Nodes returns all process names in the graph
func (g *Graph) Nodes() []string {
	nodes := make([]string, 0, len(g.nodes))
	for name := range g.nodes {
		nodes = append(nodes, name)
	}
	return nodes
}

// Dependencies returns the dependencies of a process
func (g *Graph) Dependencies(name string) []string {
	deps, exists := g.nodes[name]
	if !exists {
		return nil
	}
	return deps
}

// Validate checks if the graph is valid (all dependencies exist, no self-dependencies)
func (g *Graph) Validate() error {
	for name, deps := range g.nodes {
		for _, dep := range deps {
			// Check if dependency exists
			if _, exists := g.nodes[dep]; !exists {
				return fmt.Errorf("process %q depends on non-existent process %q", name, dep)
			}

			// Check for self-dependency
			if dep == name {
				return fmt.Errorf("process %q has self-dependency", name)
			}
		}
	}
	return nil
}

// HasCycle checks if the graph contains any cycles using DFS
func (g *Graph) HasCycle() (bool, []string) {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	parent := make(map[string]string)

	for node := range g.nodes {
		if !visited[node] {
			if cycle, path := g.hasCycleDFS(node, visited, recStack, parent); cycle {
				return true, path
			}
		}
	}

	return false, nil
}

// hasCycleDFS performs depth-first search to detect cycles
func (g *Graph) hasCycleDFS(node string, visited, recStack map[string]bool, parent map[string]string) (bool, []string) {
	visited[node] = true
	recStack[node] = true

	for _, dep := range g.nodes[node] {
		if !visited[dep] {
			parent[dep] = node
			if cycle, path := g.hasCycleDFS(dep, visited, recStack, parent); cycle {
				return true, path
			}
		} else if recStack[dep] {
			// Found a cycle, reconstruct the path
			cycle := []string{dep}
			current := node
			for current != dep {
				cycle = append([]string{current}, cycle...)
				current = parent[current]
			}
			cycle = append([]string{dep}, cycle...) // Add dep at start to show full cycle
			return true, cycle
		}
	}

	recStack[node] = false
	return false, nil
}

// TopologicalSort returns a valid execution order using Kahn's algorithm
// Returns error if graph contains cycles
// Processes with no dependencies can start in parallel (alphabetical order for determinism)
func (g *Graph) TopologicalSort() ([]string, error) {
	// First validate the graph
	if err := g.Validate(); err != nil {
		return nil, err
	}

	// Check for cycles
	if hasCycle, cycle := g.HasCycle(); hasCycle {
		return nil, fmt.Errorf("circular dependency detected: %v", cycle)
	}

	// Calculate in-degree for each node (number of dependencies)
	inDegree := make(map[string]int)
	for node, deps := range g.nodes {
		inDegree[node] = len(deps)
	}

	// Initialize queue with nodes that have no dependencies (in-degree = 0)
	queue := make([]string, 0)
	for node := range g.nodes {
		if inDegree[node] == 0 {
			queue = append(queue, node)
		}
	}

	// Sort alphabetically for deterministic ordering
	sortAlphabetically(queue)

	result := make([]string, 0, len(g.nodes))

	// Process queue
	for len(queue) > 0 {
		// Take next node alphabetically
		node := queue[0]
		queue = queue[1:]

		result = append(result, node)

		// For each process that depends on this node, reduce its in-degree
		for dependent, deps := range g.nodes {
			if contains(deps, node) {
				inDegree[dependent]--

				// If all dependencies satisfied, add to queue
				if inDegree[dependent] == 0 {
					queue = append(queue, dependent)
					// Re-sort alphabetically for deterministic ordering
					sortAlphabetically(queue)
				}
			}
		}
	}

	// If result doesn't contain all nodes, there's a cycle
	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("graph contains cycle (incomplete topological sort)")
	}

	return result, nil
}

// sortAlphabetically sorts a slice of process names alphabetically for deterministic ordering
func sortAlphabetically(nodes []string) {
	// Simple insertion sort (queue is typically small)
	for i := 1; i < len(nodes); i++ {
		key := nodes[i]
		j := i - 1

		for j >= 0 && nodes[j] > key {
			nodes[j+1] = nodes[j]
			j--
		}
		nodes[j+1] = key
	}
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

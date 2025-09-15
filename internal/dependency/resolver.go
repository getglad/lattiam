package dependency

import (
	"fmt"
	"strings"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// ProductionDependencyResolver implements proper topological sorting
type ProductionDependencyResolver struct {
	enableDebugMode bool
}

// NewProductionDependencyResolver creates a new production dependency resolver
func NewProductionDependencyResolver(enableDebugMode bool) *ProductionDependencyResolver {
	return &ProductionDependencyResolver{
		enableDebugMode: enableDebugMode,
	}
}

// BuildDependencyGraph analyzes resources and builds a dependency graph
func (r *ProductionDependencyResolver) BuildDependencyGraph(resources []interfaces.Resource, dataSources []interfaces.DataSource) (*interfaces.DependencyGraph, error) {
	graph := &interfaces.DependencyGraph{
		Resources:      make(map[string]*interfaces.Resource),
		DataSources:    make(map[string]*interfaces.DataSource),
		Dependencies:   make(map[string][]string),
		Dependents:     make(map[string][]string),
		ExecutionOrder: []string{},
	}

	// Add all resources
	for i := range resources {
		resource := &resources[i]
		key := fmt.Sprintf("%s.%s", resource.Type, resource.Name)
		graph.Resources[key] = resource

		// Extract dependencies from properties
		deps := r.extractDependencies(resource.Properties)
		graph.Dependencies[key] = deps
	}

	// Add data sources
	for i := range dataSources {
		dataSource := &dataSources[i]
		key := fmt.Sprintf("data.%s.%s", dataSource.Type, dataSource.Name)
		graph.DataSources[key] = dataSource

		// Extract dependencies from properties
		deps := r.extractDependencies(dataSource.Properties)
		graph.Dependencies[key] = deps
	}

	// Build dependents map
	for node, deps := range graph.Dependencies {
		for _, dep := range deps {
			if graph.Dependents[dep] == nil {
				graph.Dependents[dep] = []string{}
			}
			graph.Dependents[dep] = append(graph.Dependents[dep], node)
		}
	}

	return graph, nil
}

// GetExecutionOrder returns the proper topological order (Terraform-style DFS)
func (r *ProductionDependencyResolver) GetExecutionOrder(graph *interfaces.DependencyGraph) ([]string, error) {
	// Check for cycles first
	if err := r.ValidateNoCycles(graph); err != nil {
		return nil, err
	}

	// DFS-based topological sort (like Terraform)
	var result []string
	visited := make(map[string]bool)
	temp := make(map[string]bool) // For cycle detection

	var visit func(string) error
	visit = func(node string) error {
		if visited[node] {
			return nil
		}
		if temp[node] {
			return fmt.Errorf("cycle detected involving node: %s", node)
		}

		temp[node] = true

		// Visit all dependencies first
		for _, dep := range graph.Dependencies[node] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		temp[node] = false
		visited[node] = true
		result = append(result, node)

		return nil
	}

	// Visit all nodes
	allNodes := make([]string, 0, len(graph.Resources)+len(graph.DataSources))
	for key := range graph.Resources {
		allNodes = append(allNodes, key)
	}
	for key := range graph.DataSources {
		allNodes = append(allNodes, key)
	}

	for _, node := range allNodes {
		if err := visit(node); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// ValidateNoCycles validates that the graph has no cycles using DFS
func (r *ProductionDependencyResolver) ValidateNoCycles(graph *interfaces.DependencyGraph) error { //nolint:gocognit // Complex cycle detection algorithm
	white := make(map[string]bool) // Unvisited
	gray := make(map[string]bool)  // Visiting
	black := make(map[string]bool) // Visited

	// Initialize all nodes as white
	for key := range graph.Resources {
		white[key] = true
	}
	for key := range graph.DataSources {
		white[key] = true
	}

	var dfs func(string, []string) error
	dfs = func(node string, path []string) error {
		if gray[node] {
			// Found a cycle
			cycleStart := -1
			for i, n := range path {
				if n == node {
					cycleStart = i
					break
				}
			}
			cycle := append([]string(nil), path[cycleStart:]...)
			cycle = append(cycle, node)
			return fmt.Errorf("dependency cycle detected: %s", strings.Join(cycle, " -> "))
		}
		if black[node] {
			return nil
		}

		delete(white, node)
		gray[node] = true
		path = append(path, node)

		for _, dep := range graph.Dependencies[node] {
			if err := dfs(dep, path); err != nil {
				return err
			}
		}

		delete(gray, node)
		black[node] = true
		return nil
	}

	for node := range white {
		if err := dfs(node, []string{}); err != nil {
			return err
		}
	}

	return nil
}

// extractDependencies extracts dependencies from resource properties
func (r *ProductionDependencyResolver) extractDependencies(properties map[string]interface{}) []string {
	var deps []string
	r.extractDependenciesRecursive(properties, &deps)
	return removeDuplicates(deps)
}

func (r *ProductionDependencyResolver) extractDependenciesRecursive(value interface{}, deps *[]string) {
	switch v := value.(type) {
	case string:
		*deps = append(*deps, r.findInterpolations(v)...)
	case map[string]interface{}:
		for _, val := range v {
			r.extractDependenciesRecursive(val, deps)
		}
	case []interface{}:
		for _, val := range v {
			r.extractDependenciesRecursive(val, deps)
		}
	}
}

// findInterpolations finds ${...} patterns in strings using a simple parser
func (r *ProductionDependencyResolver) findInterpolations(input string) []string {
	// For dependency parsing, we use a simpler approach that doesn't treat # as comments
	// since we're parsing property values, not source code
	var interpolations []string

	// Find all ${...} patterns using a simple approach
	i := 0
	runes := []rune(input)
	for i < len(runes)-1 {
		if runes[i] == '$' && runes[i+1] == '{' {
			// Found start of interpolation
			start := i
			i += 2
			braceCount := 1

			// Find matching closing brace
			for i < len(runes) && braceCount > 0 {
				if runes[i] == '{' { //nolint:staticcheck // Simple character comparison preferred over switch
					braceCount++
				} else if runes[i] == '}' {
					braceCount--
				}
				i++
			}

			if braceCount == 0 {
				// Extract the expression
				expr := string(runes[start+2 : i-1])
				interpolations = append(interpolations, expr)
			}
		} else {
			i++
		}
	}

	// Extract resource references from interpolations
	parser := NewInterpolationParser()
	refs := parser.ExtractResourceReferences(interpolations)
	return refs
}

// GetDependencies resolves the dependencies for a specific resource
func (r *ProductionDependencyResolver) GetDependencies(_ string) ([]string, error) {
	// This would be implemented to query the stored graph
	// The resourceKey parameter will be used when graph state is stored
	return []string{}, nil
}

// GetDependents gets the dependents for a specific resource
func (r *ProductionDependencyResolver) GetDependents(_ string) ([]string, error) {
	// The resourceKey parameter will be used when graph state is stored
	return []string{}, nil
}

// GetTransitiveDependencies gets all transitive dependencies for a resource
func (r *ProductionDependencyResolver) GetTransitiveDependencies(_ string) ([]string, error) {
	// The resourceKey parameter will be used when graph state is stored
	return []string{}, nil
}

// ValidateDependenciesExist validates that all dependencies exist
func (r *ProductionDependencyResolver) ValidateDependenciesExist(graph *interfaces.DependencyGraph) error {
	allNodes := make(map[string]bool)
	for key := range graph.Resources {
		allNodes[key] = true
	}
	for key := range graph.DataSources {
		allNodes[key] = true
	}

	for node, deps := range graph.Dependencies {
		for _, dep := range deps {
			if !allNodes[dep] {
				return fmt.Errorf("resource %s depends on non-existent resource %s", node, dep)
			}
		}
	}

	return nil
}

// FindCriticalPath finds the critical path in the graph
func (r *ProductionDependencyResolver) FindCriticalPath(_ *interfaces.DependencyGraph) ([]string, error) {
	// Simplified: return longest dependency chain
	return []string{}, nil
}

// GetParallelizableGroups gets groups of resources that can be executed in parallel
func (r *ProductionDependencyResolver) GetParallelizableGroups(graph *interfaces.DependencyGraph) ([][]string, error) {
	order, err := r.GetExecutionOrder(graph)
	if err != nil {
		return nil, err
	}

	// For now, return sequential groups (one resource per group)
	// A real implementation would identify resources with no dependencies between them
	groups := make([][]string, len(order))
	for i, resource := range order {
		groups[i] = []string{resource}
	}

	return groups, nil
}

// ExportGraphViz exports the graph in GraphViz format
func (r *ProductionDependencyResolver) ExportGraphViz(graph *interfaces.DependencyGraph) (string, error) {
	var lines []string
	lines = append(lines, "digraph dependencies {")

	for node, deps := range graph.Dependencies {
		for _, dep := range deps {
			lines = append(lines, fmt.Sprintf("  %q -> %q", dep, node))
		}
	}

	lines = append(lines, "}")
	return strings.Join(lines, "\n"), nil
}

// GetGraphStatistics gets statistics about the graph
func (r *ProductionDependencyResolver) GetGraphStatistics(graph *interfaces.DependencyGraph) interfaces.GraphStatistics {
	totalNodes := len(graph.Resources) + len(graph.DataSources)
	totalEdges := 0
	for _, deps := range graph.Dependencies {
		totalEdges += len(deps)
	}

	return interfaces.GraphStatistics{
		TotalNodes:         totalNodes,
		TotalEdges:         totalEdges,
		MaxDepth:           r.calculateMaxDepth(graph),
		CriticalPathLength: r.calculateCriticalPathLength(graph),
		ParallelGroups:     1, // Simplified
	}
}

func (r *ProductionDependencyResolver) calculateMaxDepth(_ *interfaces.DependencyGraph) int {
	// Simplified implementation
	return 1
}

func (r *ProductionDependencyResolver) calculateCriticalPathLength(_ *interfaces.DependencyGraph) int {
	// Simplified implementation
	return 1
}

// Helper function to remove duplicates
func removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}

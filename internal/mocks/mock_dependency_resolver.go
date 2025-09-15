package mocks

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// MockDependencyResolver implements interfaces.DependencyResolver for testing
type MockDependencyResolver struct {
	mu             sync.RWMutex
	shouldFail     bool
	executionOrder []string
	dependencies   map[string][]string
}

// NewMockDependencyResolver creates a new mock dependency resolver
func NewMockDependencyResolver() *MockDependencyResolver {
	return &MockDependencyResolver{
		dependencies: make(map[string][]string),
	}
}

// SetShouldFail configures the mock to fail operations
func (m *MockDependencyResolver) SetShouldFail(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

// SetExecutionOrder configures the mock to return a specific execution order
func (m *MockDependencyResolver) SetExecutionOrder(order []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executionOrder = make([]string, len(order))
	copy(m.executionOrder, order)
}

// SetDependency configures the mock to use specific dependencies for a resource
func (m *MockDependencyResolver) SetDependency(resourceKey string, dependencies []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dependencies[resourceKey] = dependencies
}

// BuildDependencyGraph analyzes resources and builds a dependency graph
func (m *MockDependencyResolver) BuildDependencyGraph(resources []interfaces.Resource, dataSources []interfaces.DataSource) (*interfaces.DependencyGraph, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock dependency resolver failure")
	}

	// Create dependency graph struct
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

		// Use pre-configured dependencies if available, otherwise extract from properties
		if deps, exists := m.dependencies[key]; exists {
			graph.Dependencies[key] = deps
		} else {
			// Analyze properties for dependencies
			deps := m.extractDependencies(resource.Properties)
			graph.Dependencies[key] = deps
			m.dependencies[key] = deps
		}
	}

	// Add data sources
	for i := range dataSources {
		dataSource := &dataSources[i]
		key := fmt.Sprintf("data.%s.%s", dataSource.Type, dataSource.Name)
		graph.DataSources[key] = dataSource

		// Analyze properties for dependencies
		deps := m.extractDependencies(dataSource.Properties)
		graph.Dependencies[key] = deps
		m.dependencies[key] = deps
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

// GetExecutionOrder returns the order in which resources should be executed
func (m *MockDependencyResolver) GetExecutionOrder(graph *interfaces.DependencyGraph) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock execution order failure")
	}

	// Always use the actual graph's dependencies for topological sort
	// This ensures destruction order (with inverted dependencies) works correctly
	return m.topologicalSort(graph.Dependencies), nil
}

// GetDependencies resolves the dependencies for a specific resource
func (m *MockDependencyResolver) GetDependencies(resourceKey string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock resolve dependencies failure")
	}

	if deps, exists := m.dependencies[resourceKey]; exists {
		result := make([]string, len(deps))
		copy(result, deps)
		return result, nil
	}

	return []string{}, nil
}

// GetDependents gets the dependents for a specific resource
func (m *MockDependencyResolver) GetDependents(_ string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock get dependents failure")
	}

	// Mock implementation - return empty for simplicity
	return []string{}, nil
}

// GetTransitiveDependencies gets all transitive dependencies for a resource
func (m *MockDependencyResolver) GetTransitiveDependencies(resourceKey string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock get transitive dependencies failure")
	}

	// Mock implementation - return direct dependencies for simplicity
	return m.GetDependencies(resourceKey)
}

// ValidateNoCycles validates that the graph has no cycles
func (m *MockDependencyResolver) ValidateNoCycles(_ *interfaces.DependencyGraph) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return fmt.Errorf("mock validate no cycles failure")
	}

	// Mock implementation - assume no cycles
	return nil
}

// ValidateDependenciesExist validates that all dependencies exist
func (m *MockDependencyResolver) ValidateDependenciesExist(_ *interfaces.DependencyGraph) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return fmt.Errorf("mock validate dependencies exist failure")
	}

	// Mock implementation - assume all dependencies exist
	return nil
}

// FindCriticalPath finds the critical path in the graph
func (m *MockDependencyResolver) FindCriticalPath(_ *interfaces.DependencyGraph) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock find critical path failure")
	}

	// Mock implementation - return empty path
	return []string{}, nil
}

// GetParallelizableGroups gets groups of resources that can be executed in parallel
func (m *MockDependencyResolver) GetParallelizableGroups(graph *interfaces.DependencyGraph) ([][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock get parallelizable groups failure")
	}

	// Mock implementation - return single sequential group
	order, err := m.GetExecutionOrder(graph)
	if err != nil {
		return nil, err
	}

	groups := make([][]string, len(order))
	for i, resource := range order {
		groups[i] = []string{resource}
	}

	return groups, nil
}

// ExportGraphViz exports the graph in GraphViz format
func (m *MockDependencyResolver) ExportGraphViz(_ *interfaces.DependencyGraph) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return "", fmt.Errorf("mock export graphviz failure")
	}

	// Mock implementation - return simple dot format
	return "digraph G { /* mock graph */ }", nil
}

// GetGraphStatistics gets statistics about the graph
func (m *MockDependencyResolver) GetGraphStatistics(graph *interfaces.DependencyGraph) interfaces.GraphStatistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Mock implementation - return simple stats
	return interfaces.GraphStatistics{
		TotalNodes:         len(graph.Resources) + len(graph.DataSources),
		TotalEdges:         len(graph.Dependencies),
		MaxDepth:           1,
		CriticalPathLength: 1,
		ParallelGroups:     1,
	}
}

// extractDependencies extracts dependencies from resource properties
func (m *MockDependencyResolver) extractDependencies(properties map[string]interface{}) []string {
	var deps []string

	for _, value := range properties {
		switch v := value.(type) {
		case string:
			deps = append(deps, m.findInterpolations(v)...)
		case map[string]interface{}:
			deps = append(deps, m.extractDependencies(v)...)
		}
	}

	return deps
}

// findInterpolations finds ${...} patterns in strings
func (m *MockDependencyResolver) findInterpolations(input string) []string {
	var deps []string

	// Simple pattern matching for ${resource_type.resource_name.attribute}
	start := 0
	for {
		startIdx := strings.Index(input[start:], "${")
		if startIdx == -1 {
			break
		}
		startIdx += start

		endIdx := strings.Index(input[startIdx:], "}")
		if endIdx == -1 {
			break
		}
		endIdx += startIdx

		interpolation := input[startIdx+2 : endIdx]

		// Extract resource reference
		parts := strings.Split(interpolation, ".")
		if len(parts) >= 2 {
			// Skip function calls
			if !strings.Contains(interpolation, "(") {
				resourceKey := fmt.Sprintf("%s.%s", parts[0], parts[1])
				deps = append(deps, resourceKey)
			}
		}

		start = endIdx + 1
	}

	return deps
}

// topologicalSort performs a proper topological sort using depth-first search
func (m *MockDependencyResolver) topologicalSort(graph map[string][]string) []string {
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	var result []string

	// Helper function for depth-first search
	var visit func(node string) bool
	visit = func(node string) bool {
		if tempMark[node] {
			// Cycle detected
			return false
		}
		if visited[node] {
			return true
		}

		tempMark[node] = true

		// Visit all dependencies first
		if deps, exists := graph[node]; exists {
			for _, dep := range deps {
				if !visit(dep) {
					return false
				}
			}
		}

		tempMark[node] = false
		visited[node] = true

		// Add to result (this gives us reverse topological order)
		result = append(result, node)
		return true
	}

	// Visit all nodes
	for node := range graph {
		if !visited[node] {
			if !visit(node) {
				// Cycle detected, fallback to simple ordering
				return m.simpleSort(graph)
			}
		}
	}

	// Reverse the result to get correct topological order
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// simpleSort provides a fallback sorting method when cycles are detected
func (m *MockDependencyResolver) simpleSort(graph map[string][]string) []string {
	result := make([]string, 0, len(graph))
	for node := range graph {
		result = append(result, node)
	}
	return result
}

package interfaces

import (
	"time"
)

// DependencyResolver defines the interface for resolving resource dependencies
type DependencyResolver interface {
	// Graph building
	BuildDependencyGraph(resources []Resource, dataSources []DataSource) (*DependencyGraph, error)
	GetExecutionOrder(graph *DependencyGraph) ([]string, error)

	// Dependency queries
	GetDependencies(resourceKey string) ([]string, error)
	GetDependents(resourceKey string) ([]string, error)
	GetTransitiveDependencies(resourceKey string) ([]string, error)

	// Validation
	ValidateNoCycles(graph *DependencyGraph) error
	ValidateDependenciesExist(graph *DependencyGraph) error

	// Analysis
	FindCriticalPath(graph *DependencyGraph) ([]string, error)
	GetParallelizableGroups(graph *DependencyGraph) ([][]string, error)

	// Debugging
	ExportGraphViz(graph *DependencyGraph) (string, error)
	GetGraphStatistics(graph *DependencyGraph) GraphStatistics
}

// DependencyGraph represents a graph of resource dependencies
type DependencyGraph struct {
	Resources      map[string]*Resource
	DataSources    map[string]*DataSource
	Dependencies   map[string][]string // resource -> dependencies
	Dependents     map[string][]string // resource -> dependents
	ExecutionOrder []string            // topologically sorted order
}

// GraphStatistics provides statistics about a dependency graph
type GraphStatistics struct {
	TotalNodes         int
	TotalEdges         int
	MaxDepth           int
	CriticalPathLength int
	ParallelGroups     int
}

// DependencyResolverCall represents a call to the DependencyResolver for tracking in mocks
type DependencyResolverCall struct {
	Method    string
	GraphID   string
	Timestamp time.Time
	Error     error
}

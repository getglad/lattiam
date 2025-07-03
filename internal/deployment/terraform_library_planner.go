package deployment

import (
	"context"
	"fmt"
	"strings"

	"github.com/dominikbraun/graph"

	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// TerraformLibraryPlanner uses sophisticated dependency analysis directly from JSON
// No disk I/O, no HCL conversion - pure in-memory analysis
type TerraformLibraryPlanner struct {
	providerManager *protocol.ProviderManager
	logger          Logger
}

// NewTerraformLibraryPlanner creates a planner using direct JSON analysis
func NewTerraformLibraryPlanner(providerManager *protocol.ProviderManager, logger Logger) *TerraformLibraryPlanner {
	return &TerraformLibraryPlanner{
		providerManager: providerManager,
		logger:          logger,
	}
}

// Use the existing interpolationPattern from interpolation.go

// AnalyzeResourceDependenciesWithTerraform uses sophisticated DAG-based dependency analysis
// directly from JSON configuration without any disk I/O
func (p *TerraformLibraryPlanner) AnalyzeResourceDependenciesWithTerraform(
	_ context.Context, // ctx - reserved for future use
	resources []Resource,
) ([]Resource, error) {
	p.logger.Infof("Starting sophisticated JSON-based dependency analysis for %d resources", len(resources))

	// Build the dependency graph
	g, resourceMap, err := p.buildDependencyGraph(resources)
	if err != nil {
		return nil, err
	}

	// Get topological order
	ordered, err := graph.TopologicalSort(g)
	if err != nil {
		return nil, fmt.Errorf("failed to get topological order (circular dependency?): %w", err)
	}

	// Convert ordered keys back to resources
	result := p.convertOrderedKeysToResources(ordered, resourceMap)

	// Log analysis results
	p.logAnalysisResults(result, resources, ordered, resourceMap)

	return result, nil
}

// buildDependencyGraph creates a DAG from resources and their dependencies
func (p *TerraformLibraryPlanner) buildDependencyGraph(resources []Resource) (graph.Graph[string, string], map[string]*Resource, error) {
	g := graph.New(graph.StringHash, graph.Directed(), graph.Acyclic())
	resourceMap := make(map[string]*Resource)

	// Add all resources as vertices
	if err := p.addResourceVertices(g, resources, resourceMap); err != nil {
		return nil, nil, err
	}

	// Add dependency edges
	if err := p.addDependencyEdges(g, resources, resourceMap); err != nil {
		return nil, nil, err
	}

	return g, resourceMap, nil
}

// addResourceVertices adds all resources as vertices to the graph
func (p *TerraformLibraryPlanner) addResourceVertices(g graph.Graph[string, string], resources []Resource, resourceMap map[string]*Resource) error {
	for i := range resources {
		resource := &resources[i]
		key := p.resourceKey(resource)
		resourceMap[key] = resource

		if err := g.AddVertex(key); err != nil {
			return fmt.Errorf("failed to add vertex %s: %w", key, err)
		}
	}
	return nil
}

// addDependencyEdges analyzes resources and adds dependency edges to the graph
func (p *TerraformLibraryPlanner) addDependencyEdges(g graph.Graph[string, string], resources []Resource, resourceMap map[string]*Resource) error {
	for i := range resources {
		resource := &resources[i]
		resourceKey := p.resourceKey(resource)

		// Find all dependencies in this resource's properties
		deps := p.findDependenciesInJSON(resource.Properties)

		for _, dep := range deps {
			if err := p.addSingleDependencyEdge(g, dep, resourceKey, resourceMap); err != nil {
				return err
			}
		}
	}
	return nil
}

// addSingleDependencyEdge adds a single dependency edge if the dependency exists
func (p *TerraformLibraryPlanner) addSingleDependencyEdge(g graph.Graph[string, string], dep, resourceKey string, resourceMap map[string]*Resource) error {
	if _, exists := resourceMap[dep]; exists {
		// Add edge from dependency to dependent
		if err := g.AddEdge(dep, resourceKey); err != nil {
			return fmt.Errorf("failed to add edge %s -> %s: %w", dep, resourceKey, err)
		}
		p.logger.Debugf("Found dependency: %s -> %s", dep, resourceKey)
	} else {
		// Log skipped dependencies (e.g., data sources, external resources)
		p.logger.Debugf("Skipping dependency %s (not in resource list) for %s", dep, resourceKey)
	}
	return nil
}

// convertOrderedKeysToResources converts ordered resource keys back to Resource slice
func (p *TerraformLibraryPlanner) convertOrderedKeysToResources(ordered []string, resourceMap map[string]*Resource) []Resource {
	result := make([]Resource, 0, len(ordered))
	for _, key := range ordered {
		if resource, exists := resourceMap[key]; exists {
			result = append(result, *resource)
		}
	}
	return result
}

// logAnalysisResults logs the results of dependency analysis
func (p *TerraformLibraryPlanner) logAnalysisResults(result, resources []Resource, ordered []string, resourceMap map[string]*Resource) {
	p.logger.Infof("Successfully analyzed dependencies using sophisticated DAG, ordered %d resources (input: %d)", len(result), len(resources))

	if len(result) != len(resources) {
		p.logger.Infof("WARNING: Resource count mismatch! Missing resources in DAG output")
		p.logMissingResources(ordered, resourceMap)
	}
}

// logMissingResources logs which resources are missing from the ordered output
func (p *TerraformLibraryPlanner) logMissingResources(ordered []string, resourceMap map[string]*Resource) {
	orderedSet := make(map[string]bool)
	for _, key := range ordered {
		orderedSet[key] = true
	}

	for key := range resourceMap {
		if !orderedSet[key] {
			p.logger.Infof("  Missing resource: %s", key)
		}
	}
}

// resourceKey generates a unique key for a resource
func (p *TerraformLibraryPlanner) resourceKey(resource *Resource) string {
	return fmt.Sprintf("%s.%s", resource.Type, resource.Name)
}

// findDependenciesInJSON finds all resource dependencies in JSON properties
// by analyzing interpolation patterns directly in the data structure
func (p *TerraformLibraryPlanner) findDependenciesInJSON(properties map[string]interface{}) []string {
	depMap := make(map[string]bool)
	p.findDepsRecursive(properties, depMap)

	// Convert to slice and remove duplicates
	deps := make([]string, 0, len(depMap))
	for dep := range depMap {
		deps = append(deps, dep)
	}

	return deps
}

// findDepsRecursive recursively searches JSON structure for interpolation patterns
func (p *TerraformLibraryPlanner) findDepsRecursive(value interface{}, deps map[string]bool) {
	switch v := value.(type) {
	case string:
		// Look for interpolation patterns like ${resource.name.attribute}
		matches := interpolationPattern.FindAllStringSubmatch(v, -1)
		for _, match := range matches {
			if len(match) > 1 {
				// Parse the interpolation content
				interpolation := strings.TrimSpace(match[1])
				if dep := p.parseInterpolationReference(interpolation); dep != "" {
					deps[dep] = true
				}
			}
		}
	case map[string]interface{}:
		// Recursively analyze nested objects
		for _, val := range v {
			p.findDepsRecursive(val, deps)
		}
	case []interface{}:
		// Recursively analyze arrays
		for _, val := range v {
			p.findDepsRecursive(val, deps)
		}
	}
}

// parseInterpolationReference parses interpolation content to extract resource reference
// Handles patterns like: resource_type.name.attribute, data.type.name.attribute, etc.
func (p *TerraformLibraryPlanner) parseInterpolationReference(interpolation string) string {
	// Skip function calls (they contain parentheses)
	if strings.Contains(interpolation, "(") {
		return ""
	}

	parts := strings.Split(interpolation, ".")

	// Skip non-resource references
	if len(parts) < 3 {
		return ""
	}

	// Skip variables and locals (but NOT data sources - they're handled separately)
	if parts[0] == "var" || parts[0] == "local" {
		return ""
	}

	// Handle data source references: data.type.name.attribute
	if parts[0] == "data" && len(parts) >= 4 {
		// Note: Data sources are not in the resource graph but we shouldn't skip them entirely
		// This helps with logging and debugging
		return fmt.Sprintf("data.%s.%s", parts[1], parts[2])
	}

	// For resource references, expect: resource_type.name.attribute
	if len(parts) >= 3 {
		return fmt.Sprintf("%s.%s", parts[0], parts[1])
	}

	return ""
}

package dependency

import (
	"fmt"
	"strings"
	"unicode"
)

// InterpolationParser provides reliable parsing of Terraform-style interpolations
type InterpolationParser struct{}

// NewInterpolationParser creates a new interpolation parser
func NewInterpolationParser() *InterpolationParser {
	return &InterpolationParser{}
}

// ParseInterpolations extracts valid interpolation expressions from input
// It properly handles:
// - Comments (both // and /* */ style)
// - String literals
// - Nested expressions
// - Function calls
//
//nolint:gocognit,gocyclo,funlen // Complex parsing logic requires handling multiple cases
func (p *InterpolationParser) ParseInterpolations(input string) ([]string, error) {
	interpolations := []string{}
	runes := []rune(input)
	i := 0
	inString := false
	var stringChar rune

	for i < len(runes) {
		// Handle comments
		if !inString && i+1 < len(runes) {
			if runes[i] == '/' && runes[i+1] == '/' {
				// Skip to end of line
				for i < len(runes) && runes[i] != '\n' {
					i++
				}
				continue
			}
			if runes[i] == '/' && runes[i+1] == '*' {
				// Skip to */
				i += 2
				for i+1 < len(runes) && (runes[i] != '*' || runes[i+1] != '/') {
					i++
				}
				if i+1 < len(runes) {
					i += 2
				}
				continue
			}
		}

		// Handle hash comments
		if !inString && runes[i] == '#' {
			// fmt.Printf("DEBUG: Found # at position %d, skipping line\n", i)
			// Skip to end of line
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			continue
		}

		// Handle string literals
		if runes[i] == '"' || runes[i] == '\'' || runes[i] == '`' {
			if !inString {
				inString = true
				stringChar = runes[i]
			} else if runes[i] == stringChar && (i == 0 || runes[i-1] != '\\') {
				inString = false
			}
		}

		// Look for interpolations
		if i+1 < len(runes) && runes[i] == '$' && runes[i+1] == '{' {
			// fmt.Printf("DEBUG: Found ${ at position %d\n", i)
			// Find the closing brace
			start := i
			i += 2
			braceCount := 1

			for i < len(runes) && braceCount > 0 {
				if runes[i] == '{' { //nolint:staticcheck // Simple character comparison preferred over switch
					braceCount++
				} else if runes[i] == '}' {
					braceCount--
				}
				i++
			}

			if braceCount != 0 {
				return nil, fmt.Errorf("unclosed interpolation at position %d", start)
			}

			// Extract the interpolation content
			expr := string(runes[start+2 : i-1])
			if expr == "" {
				return nil, fmt.Errorf("empty interpolation at position %d", start)
			}
			if p.isValidExpression(expr) {
				interpolations = append(interpolations, expr)
			}
			continue
		}

		i++
	}

	return interpolations, nil
}

// ExtractResourceReferences extracts resource references from interpolations
// Filters out function calls and returns only direct resource references
func (p *InterpolationParser) ExtractResourceReferences(interpolations []string) []string {
	refs := []string{}
	seen := make(map[string]bool)

	for _, interp := range interpolations {
		// Skip if contains function call
		if strings.Contains(interp, "(") {
			continue
		}

		// Extract resource reference (type.name)
		parts := strings.Split(interp, ".")
		if len(parts) >= 2 {
			resourceKey := fmt.Sprintf("%s.%s", parts[0], parts[1])
			if !seen[resourceKey] {
				refs = append(refs, resourceKey)
				seen[resourceKey] = true
			}
		}
	}

	return refs
}

// isValidExpression checks if an expression contains valid characters
func (p *InterpolationParser) isValidExpression(expr string) bool {
	if expr == "" {
		return false
	}

	// Must start with a letter or underscore
	runes := []rune(expr)
	if !unicode.IsLetter(runes[0]) && runes[0] != '_' {
		return false
	}

	// Check for at least one valid identifier character
	hasIdentifier := false
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			hasIdentifier = true
			break
		}
	}

	return hasIdentifier
}

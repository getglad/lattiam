package helpers

import (
	"os"
	"strings"
	"testing"
)

// TestCategory represents a category of tests
type TestCategory string

const (
	// Test categories
	CategoryUnit        TestCategory = "unit"
	CategoryIntegration TestCategory = "integration"
	CategoryAWS         TestCategory = "aws"
	CategoryLocalStack  TestCategory = "localstack"
	CategorySlow        TestCategory = "slow"
	CategoryFast        TestCategory = "fast"
	CategoryParallel    TestCategory = "parallel"
	CategorySerial      TestCategory = "serial"
	CategorySmoke       TestCategory = "smoke"
	CategoryFull        TestCategory = "full"
)

// Environment variable for test categories
const EnvTestCategories = "LATTIAM_TEST_CATEGORIES"

// SkipIfNotCategory skips the test if it doesn't match the specified category
func SkipIfNotCategory(t *testing.T, category TestCategory) {
	t.Helper()

	if !IsTestCategoryEnabled(category) {
		t.Skipf("Skipping test: category '%s' not enabled", category)
	}
}

// SkipIfCategory skips the test if it matches the specified category
func SkipIfCategory(t *testing.T, category TestCategory) {
	t.Helper()

	if IsTestCategoryEnabled(category) {
		t.Skipf("Skipping test: category '%s' is excluded", category)
	}
}

// RequireCategories skips the test unless all specified categories are enabled
func RequireCategories(t *testing.T, categories ...TestCategory) {
	t.Helper()

	var missing []string
	for _, cat := range categories {
		if !IsTestCategoryEnabled(cat) {
			missing = append(missing, string(cat))
		}
	}

	if len(missing) > 0 {
		t.Skipf("Skipping test: required categories not enabled: %s", strings.Join(missing, ", "))
	}
}

// RequireAnyCategory skips the test unless at least one category is enabled
func RequireAnyCategory(t *testing.T, categories ...TestCategory) {
	t.Helper()

	for _, cat := range categories {
		if IsTestCategoryEnabled(cat) {
			return
		}
	}

	cats := make([]string, len(categories))
	for i, c := range categories {
		cats[i] = string(c)
	}
	t.Skipf("Skipping test: none of the required categories enabled: %s", strings.Join(cats, ", "))
}

// IsTestCategoryEnabled checks if a test category is enabled
func IsTestCategoryEnabled(category TestCategory) bool {
	// If no categories specified, run all tests
	categoriesEnv := os.Getenv(EnvTestCategories)
	if categoriesEnv == "" {
		return true
	}

	// Check if the category is in the enabled list
	enabledCategories := strings.Split(categoriesEnv, ",")
	for _, enabled := range enabledCategories {
		enabled = strings.TrimSpace(enabled)
		if enabled == string(category) || enabled == "all" {
			return true
		}
	}

	return false
}

// GetEnabledCategories returns the list of enabled test categories
func GetEnabledCategories() []TestCategory {
	categoriesEnv := os.Getenv(EnvTestCategories)
	if categoriesEnv == "" {
		return []TestCategory{} // Empty means all enabled
	}

	parts := strings.Split(categoriesEnv, ",")
	categories := make([]TestCategory, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			categories = append(categories, TestCategory(part))
		}
	}

	return categories
}

// MarkTestCategory marks a test as belonging to specific categories (for documentation)
func MarkTestCategory(t *testing.T, categories ...TestCategory) {
	t.Helper()

	// Log the categories for test reporting
	cats := make([]string, len(categories))
	for i, c := range categories {
		cats[i] = string(c)
	}
	t.Logf("Test categories: %s", strings.Join(cats, ", "))
}

// TestCategoryEnvironment represents test environment requirements
type TestCategoryEnvironment struct {
	RequiresAWS        bool
	RequiresLocalStack bool
	RequiresNetwork    bool
	MaxDuration        string
}

// GetCategoryEnvironment returns environment requirements for a category
func GetCategoryEnvironment(category TestCategory) TestCategoryEnvironment {
	switch category {
	case CategoryAWS:
		return TestCategoryEnvironment{
			RequiresAWS:     true,
			RequiresNetwork: true,
		}
	case CategoryLocalStack:
		return TestCategoryEnvironment{
			RequiresLocalStack: true,
			RequiresNetwork:    true,
		}
	case CategoryIntegration:
		return TestCategoryEnvironment{
			RequiresNetwork: true,
		}
	case CategoryUnit:
		return TestCategoryEnvironment{
			// Unit tests have no special requirements
		}
	case CategorySlow:
		return TestCategoryEnvironment{
			MaxDuration: "10m",
		}
	case CategoryFast:
		return TestCategoryEnvironment{
			MaxDuration: "30s",
		}
	default:
		return TestCategoryEnvironment{}
	}
}

// Example usage in tests:
// func TestAWSDeployment(t *testing.T) {
//     MarkTestCategory(t, CategoryIntegration, CategoryAWS)
//     RequireCategories(t, CategoryIntegration)
//     SkipIfNotCategory(t, CategoryAWS)
//     // ... test code
// }
//
// Run specific categories:
// LATTIAM_TEST_CATEGORIES=unit,fast go test ./...
// LATTIAM_TEST_CATEGORIES=integration,localstack go test ./...
// LATTIAM_TEST_CATEGORIES=smoke go test ./...

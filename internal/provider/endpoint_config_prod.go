//go:build !localstack && !dev
// +build !localstack,!dev

package provider

// Stub for production builds - LocalStack support is not included
type LocalStackStrategy struct{}

func NewLocalStackStrategy() *LocalStackStrategy {
	return &LocalStackStrategy{}
}

func (s *LocalStackStrategy) ShouldApply() bool {
	return false
}

func (s *LocalStackStrategy) ConfigureEndpoint(_ *EnvironmentConfig) {
	// No-op in production
}

func (s *LocalStackStrategy) GetHealthCheckURL() string {
	return ""
}

func includeLocalStackStrategy() bool {
	return false
}

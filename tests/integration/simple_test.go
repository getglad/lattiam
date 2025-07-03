//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SimpleTestSuite struct {
	suite.Suite
}

func TestSimpleTestSuite(t *testing.T) {
	suite.Run(t, new(SimpleTestSuite))
}

func (s *SimpleTestSuite) TestSimple() {
	s.T().Log("This is a simple test")
	s.True(true)
}

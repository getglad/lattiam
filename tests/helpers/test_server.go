package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver"
)

// TestServer wraps an API server for testing
type TestServer struct {
	Server *apiserver.APIServer
	URL    string
	Port   int
}

// StartTestServer starts a new API server on a random port for testing
func StartTestServer(t *testing.T) *TestServer {
	t.Helper()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create and start the server
	server, err := apiserver.NewAPIServer(port)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			errChan <- fmt.Errorf("server error: %w", err)
		}
		close(errChan)
	}()

	// Check for immediate errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Failed to start server: %v", err)
		}
	default:
	}

	// Wait for server to be ready
	url := fmt.Sprintf("http://localhost:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if i == maxRetries-1 {
			t.Fatalf("Server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return &TestServer{
		Server: server,
		URL:    url,
		Port:   port,
	}
}

// Stop gracefully shuts down the test server
func (ts *TestServer) Stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ts.Server.Shutdown(ctx); err != nil {
		t.Logf("Failed to stop server: %v", err)
	}
}

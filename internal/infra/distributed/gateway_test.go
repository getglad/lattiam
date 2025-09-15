//go:build integration
// +build integration

package distributed_test

import (
	"net"
	"os/exec"
	"testing"
)

func TestGatewayDiscovery(t *testing.T) {
	t.Parallel()
	// Check network interfaces
	t.Log("=== Network Interfaces ===")
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to get interfaces: %v", err)
	}

	for _, iface := range interfaces {
		t.Logf("Interface: %s", iface.Name)
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			t.Logf("  Address: %s", addr.String())
		}
	}

	// Check default gateway
	t.Log("\n=== Default Gateway ===")
	cmd := exec.Command("ip", "route", "show", "default")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to get default route: %v", err)
	} else {
		t.Logf("Default route: %s", output)
	}

	// Try to find the host gateway
	t.Log("\n=== Host Gateway ===")
	cmd = exec.Command("getent", "hosts", "host.docker.internal")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("host.docker.internal not found: %v", err)
	} else {
		t.Logf("host.docker.internal: %s", output)
	}

	// Check /etc/hosts
	t.Log("\n=== /etc/hosts ===")
	cmd = exec.Command("grep", "host", "/etc/hosts")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("No host entries in /etc/hosts: %v", err)
	} else {
		t.Logf("Host entries: %s", output)
	}
}

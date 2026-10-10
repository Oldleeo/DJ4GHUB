package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeHTTPFromInterfaceUsesRequestedInterface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := probeHTTPFromInterface(ctx, loopbackInterfaceName(t), "127.0.0.1", server.URL); err != nil {
		t.Fatalf("probeHTTPFromInterface() error = %v", err)
	}
}

func TestProbeHTTPFromInterfaceRejectsInvalidIPv4(t *testing.T) {
	err := probeHTTPFromInterface(context.Background(), loopbackInterfaceName(t), "not-an-ip", "http://127.0.0.1")
	if err == nil {
		t.Fatal("probeHTTPFromInterface() error = nil, want invalid IPv4 error")
	}
}

func TestProbeAnyTargetAcceptsOneSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	target, err := probeAnyTarget(ctx, loopbackInterfaceName(t), "127.0.0.1", []string{"http://127.0.0.1:1", server.URL})
	if err != nil {
		t.Fatalf("probeAnyTarget() error = %v", err)
	}
	if target != server.URL {
		t.Fatalf("probeAnyTarget() target = %q, want %q", target, server.URL)
	}
}

func loopbackInterfaceName(t *testing.T) string {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces() error = %v", err)
	}
	for _, candidate := range interfaces {
		addresses, err := candidate.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.Equal(net.IPv4(127, 0, 0, 1)) {
				return candidate.Name
			}
		}
	}
	t.Fatal("loopback interface not found")
	return ""
}

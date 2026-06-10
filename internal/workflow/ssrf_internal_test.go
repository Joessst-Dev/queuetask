package workflow

import (
	"net/http"
	"net/url"
	"testing"
)

func TestCheckSSRFHost_BlockedRanges(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // IPv4 loopback
		"127.255.255.255", // loopback subnet boundary
		"::1",             // IPv6 loopback
		"169.254.1.1",     // link-local / AWS IMDS
		"169.254.169.254", // AWS IMDS exact address
		"10.0.0.1",        // RFC 1918
		"10.255.255.255",  // RFC 1918 boundary
		"172.16.0.1",      // RFC 1918
		"172.31.255.255",  // RFC 1918 boundary
		"192.168.0.1",     // RFC 1918
		"0.0.0.1",         // "this" network
		"100.64.0.1",      // CGNAT
		"fc00::1",         // IPv6 ULA
		"fd00::1",         // IPv6 ULA
	}
	for _, ip := range blocked {
		t.Run(ip, func(t *testing.T) {
			if err := checkSSRFHost(ip); err == nil {
				t.Errorf("checkSSRFHost(%q) returned nil, expected error", ip)
			}
		})
	}
}

func TestCheckSSRFHost_AllowedHosts(t *testing.T) {
	allowed := []string{
		"8.8.8.8",          // public DNS
		"1.1.1.1",          // public DNS
		"203.0.113.1",      // TEST-NET-3 (documentation range — not in blocklist)
		"example.com",      // hostname — DNS rebinding is a known limitation
		"api.example.com",  // hostname
		"",                 // empty — no-op
	}
	for _, h := range allowed {
		t.Run(h, func(t *testing.T) {
			if err := checkSSRFHost(h); err != nil {
				t.Errorf("checkSSRFHost(%q) returned unexpected error: %v", h, err)
			}
		})
	}
}

func TestSSRFRedirectCheck_BlocksPrivateIP(t *testing.T) {
	privateTargets := []string{
		"http://10.0.0.1/api",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.168.1.1/",
		"http://[::1]/",
	}
	for _, target := range privateTargets {
		t.Run(target, func(t *testing.T) {
			u, err := url.Parse(target)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", target, err)
			}
			req := &http.Request{URL: u}
			if err := ssrfRedirectCheck(req, nil); err == nil {
				t.Errorf("ssrfRedirectCheck did not block redirect to %q", target)
			}
		})
	}
}

func TestSSRFRedirectCheck_AllowsPublicIP(t *testing.T) {
	publicTargets := []string{
		"http://8.8.8.8/",
		"https://api.example.com/v1",
		"http://203.0.113.1/resource",
	}
	for _, target := range publicTargets {
		t.Run(target, func(t *testing.T) {
			u, err := url.Parse(target)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", target, err)
			}
			req := &http.Request{URL: u}
			if err := ssrfRedirectCheck(req, nil); err != nil {
				t.Errorf("ssrfRedirectCheck unexpectedly blocked redirect to %q: %v", target, err)
			}
		})
	}
}

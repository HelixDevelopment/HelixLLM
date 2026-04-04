package proxy_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/proxy"
)

func TestApply_SetsHTTPProxy(t *testing.T) {
	os.Unsetenv("HTTP_PROXY")
	defer os.Unsetenv("HTTP_PROXY")

	proxy.Apply(proxy.Config{HTTPProxy: "http://proxy.example.com:3128"})

	if got := os.Getenv("HTTP_PROXY"); got != "http://proxy.example.com:3128" {
		t.Errorf("HTTP_PROXY = %q, want %q", got, "http://proxy.example.com:3128")
	}
}

func TestApply_SetsHTTPSProxy(t *testing.T) {
	os.Unsetenv("HTTPS_PROXY")
	defer os.Unsetenv("HTTPS_PROXY")

	proxy.Apply(proxy.Config{HTTPSProxy: "https://proxy.example.com:3128"})

	if got := os.Getenv("HTTPS_PROXY"); got != "https://proxy.example.com:3128" {
		t.Errorf("HTTPS_PROXY = %q, want %q", got, "https://proxy.example.com:3128")
	}
}

func TestApply_SetsNoProxy(t *testing.T) {
	os.Unsetenv("NO_PROXY")
	defer os.Unsetenv("NO_PROXY")

	proxy.Apply(proxy.Config{NoProxy: "localhost,127.0.0.1"})

	if got := os.Getenv("NO_PROXY"); got != "localhost,127.0.0.1" {
		t.Errorf("NO_PROXY = %q, want %q", got, "localhost,127.0.0.1")
	}
}

func TestApply_EmptyFieldsDoNotOverwrite(t *testing.T) {
	os.Setenv("HTTP_PROXY", "http://original.example.com")
	defer os.Unsetenv("HTTP_PROXY")

	// Apply with empty HTTPProxy — should not clear the existing value.
	proxy.Apply(proxy.Config{})

	if got := os.Getenv("HTTP_PROXY"); got != "http://original.example.com" {
		t.Errorf("HTTP_PROXY = %q, want original value", got)
	}
}

func TestApply_AllFields(t *testing.T) {
	os.Unsetenv("HTTP_PROXY")
	os.Unsetenv("HTTPS_PROXY")
	os.Unsetenv("NO_PROXY")
	defer func() {
		os.Unsetenv("HTTP_PROXY")
		os.Unsetenv("HTTPS_PROXY")
		os.Unsetenv("NO_PROXY")
	}()

	cfg := proxy.Config{
		HTTPProxy:  "http://proxy.internal:3128",
		HTTPSProxy: "https://proxy.internal:3128",
		NoProxy:    "*.internal,localhost",
	}
	proxy.Apply(cfg)

	if got := os.Getenv("HTTP_PROXY"); got != cfg.HTTPProxy {
		t.Errorf("HTTP_PROXY = %q, want %q", got, cfg.HTTPProxy)
	}
	if got := os.Getenv("HTTPS_PROXY"); got != cfg.HTTPSProxy {
		t.Errorf("HTTPS_PROXY = %q, want %q", got, cfg.HTTPSProxy)
	}
	if got := os.Getenv("NO_PROXY"); got != cfg.NoProxy {
		t.Errorf("NO_PROXY = %q, want %q", got, cfg.NoProxy)
	}
}

func TestTransport_NonNil(t *testing.T) {
	tr := proxy.Transport(proxy.Config{})
	if tr == nil {
		t.Error("Transport() returned nil")
	}
}

func TestTransport_WithConfig_NonNil(t *testing.T) {
	tr := proxy.Transport(proxy.Config{
		HTTPProxy:  "http://proxy.example.com:3128",
		HTTPSProxy: "https://proxy.example.com:3128",
	})
	if tr == nil {
		t.Error("Transport() returned nil with non-empty config")
	}
}

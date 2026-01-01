package chat

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	// Just verify we can create it without panic and constants are sane
	m := NewManager("token", "url", "model", nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.RateLimit() != defaultRateLimitPerMin {
		t.Errorf("expected default rate limit %d, got %d", defaultRateLimitPerMin, m.RateLimit())
	}
    
    // Check constants via reflection or logic if needed, but they are private.
    // We just ensure it compiles and runs.
}

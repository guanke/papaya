package chat

import "testing"

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

func TestFormatAnswer(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{
			name: "trims extra spaces and blank lines",
			in:   "Hello world\n\n\nThanks!  ",
			out:  "Hello world\n\nThanks!",
		},
		{
			name: "normalizes windows newlines",
			in:   "Line1\r\nLine2\r\n\r\nLine3",
			out:  "Line1\nLine2\n\nLine3",
		},
	}

	for _, tt := range tests {
		got := formatAnswer(tt.in)
		if got != tt.out {
			t.Fatalf("%s: unexpected formatted text\nwant: %q\ngot:  %q", tt.name, tt.out, got)
		}
	}
}

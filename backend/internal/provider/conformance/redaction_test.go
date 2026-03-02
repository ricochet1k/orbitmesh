package conformance

import (
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		notWanted []string
		wanted    []string
	}{
		{
			name:      "authorization bearer token",
			input:     `{"authorization":"Bearer super-secret-token"}`,
			notWanted: []string{"super-secret-token"},
			wanted:    []string{"[REDACTED]"},
		},
		{
			name:      "api key field",
			input:     `x-api-key: abc123`,
			notWanted: []string{"abc123"},
			wanted:    []string{"[REDACTED]"},
		},
		{
			name:      "generic bearer",
			input:     `token used: Bearer xyz.123-abc`,
			notWanted: []string{"xyz.123-abc"},
			wanted:    []string{"Bearer [REDACTED]"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			redacted := RedactString(tt.input)
			for _, secret := range tt.notWanted {
				if strings.Contains(redacted, secret) {
					t.Fatalf("expected secret %q to be redacted; got %q", secret, redacted)
				}
			}

			for _, expected := range tt.wanted {
				if !strings.Contains(redacted, expected) {
					t.Fatalf("expected %q in redacted output; got %q", expected, redacted)
				}
			}
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	headers := map[string]string{
		"Authorization": "Bearer abc.def",
		"X-Api-Key":     "my-secret-key",
		"X-Request-Id":  "req-1",
	}

	redacted := RedactHeaders(headers)
	if redacted["Authorization"] != "Bearer [REDACTED]" {
		t.Fatalf("expected bearer authorization header to be redacted, got %q", redacted["Authorization"])
	}

	if redacted["X-Api-Key"] != "[REDACTED]" {
		t.Fatalf("expected api key header to be redacted, got %q", redacted["X-Api-Key"])
	}

	if redacted["X-Request-Id"] != "req-1" {
		t.Fatalf("expected non-sensitive header to remain unchanged, got %q", redacted["X-Request-Id"])
	}
}

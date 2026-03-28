package propagation

import (
	"testing"
)

func TestBoolVal(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		input    *bool
		def      bool
		expected bool
	}{
		{"nil returns default true", nil, true, true},
		{"nil returns default false", nil, false, false},
		{"true returns true", &trueVal, false, true},
		{"false returns false", &falseVal, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boolVal(tt.input, tt.def); got != tt.expected {
				t.Errorf("boolVal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

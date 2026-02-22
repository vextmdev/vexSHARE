package tokens

import "testing"

func TestGenerate(t *testing.T) {
	token, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(token) != 43 {
		t.Errorf("expected token length 43, got %d (%q)", len(token), token)
	}
	token2, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		provided string
		want     bool
	}{
		{"matching", "abc123", "abc123", true},
		{"mismatch", "abc123", "xyz789", false},
		{"empty expected", "", "abc123", false},
		{"empty provided", "abc123", "", false},
		{"both empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Validate(tt.expected, tt.provided)
			if got != tt.want {
				t.Errorf("Validate(%q, %q) = %v, want %v", tt.expected, tt.provided, got, tt.want)
			}
		})
	}
}

func TestGeneratePassword(t *testing.T) {
	pw, err := GeneratePassword(20)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(pw) != 20 {
		t.Errorf("expected len 20, got %d", len(pw))
	}
	if _, err := GeneratePassword(0); err == nil {
		t.Error("expected error for zero length")
	}
	if _, err := GeneratePassword(-1); err == nil {
		t.Error("expected error for negative length")
	}
	pw1, _ := GeneratePassword(16)
	pw2, _ := GeneratePassword(16)
	if pw1 == pw2 {
		t.Error("expected unique passwords")
	}
}

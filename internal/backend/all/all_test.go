package all

import (
	"slices"
	"testing"

	"herdr-dots/internal/backend"
)

func TestEveryKindIsRegistered(t *testing.T) {
	want := []string{"anthropic-api", "claude-cli", "codex-cli", "ollama", "openai-api", "openrouter"}
	if got := backend.Kinds(); !slices.Equal(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	for _, k := range want {
		b, err := backend.NewWith(k, backend.Options{})
		if err != nil || b.Kind() != k {
			t.Errorf("NewWith(%s) = %v, %v", k, b, err)
		}
	}
}

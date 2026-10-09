package dots

import "fmt"

// MemorySpec is a dot's [memory] table. Dot files without one get the defaults.
type MemorySpec struct {
	Enabled             bool `toml:"enabled"`                // read memory into prompts
	Learn               bool `toml:"learn"`                  // propose memory ops; observe dots run memory passes
	ReadProject         bool `toml:"read_project"`           // include project-scope memory
	EveryTurns          int  `toml:"every_turns"`            // observe dots: one memory pass per N new turns
	MaxPassesPerSession int  `toml:"max_passes_per_session"` // memory passes per attachment
	PromptChars         int  `toml:"prompt_chars"`           // memory block budget per run
}

func DefaultMemory() MemorySpec {
	return MemorySpec{Enabled: true, Learn: true, ReadProject: true, EveryTurns: 5, MaxPassesPerSession: 10, PromptChars: 6000}
}

func (m MemorySpec) validate() []error {
	var errs []error
	if m.EveryTurns < 1 || m.EveryTurns > 50 {
		errs = append(errs, fmt.Errorf("memory every_turns must be 1-50, got %d", m.EveryTurns))
	}
	if m.MaxPassesPerSession < 0 || m.MaxPassesPerSession > 200 {
		errs = append(errs, fmt.Errorf("memory max_passes_per_session must be 0-200, got %d", m.MaxPassesPerSession))
	}
	if m.PromptChars < 0 || m.PromptChars > 20000 {
		errs = append(errs, fmt.Errorf("memory prompt_chars must be 0-20000, got %d", m.PromptChars))
	}
	return errs
}

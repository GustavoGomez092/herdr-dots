package memory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Phase 9 residual: memory imports are someone else's file too; a TOML bomb
// is refused before the decoder sees it.
func TestImportTOMLRefusesBombsFast(t *testing.T) {
	bombs := map[string]string{
		"64 KB nested inline tables": "x = " + strings.Repeat("{a=", 64<<10/4) + "1" + strings.Repeat("}", 64<<10/4),
		"64 KB dotted key":           strings.Repeat("a.", 32<<10) + "a = 1",
	}
	for name, bomb := range bombs {
		start := time.Now()
		_, err := ImportTOML([]byte("version = 1\n" + bomb + "\n"))
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%s: took %v", name, took)
		}
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		if err != nil && len(err.Error()) > 400 {
			t.Errorf("%s: error not capped (%d bytes)", name, len(err.Error()))
		}
	}
}

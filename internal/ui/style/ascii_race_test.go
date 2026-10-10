package style

import (
	"sync"
	"testing"
)

// U6 (post-rc hardening): SetASCII and Sym may run on different goroutines
// (a test toggling the mode while a View renders); the switch is atomic, so
// go test -race stays quiet.
func TestSetASCIIIsSafeForConcurrentUse(t *testing.T) {
	old := SetASCII(false)
	defer SetASCII(old)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			SetASCII(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = Sym("◆")
			_ = ASCII()
		}
	}()
	wg.Wait()
}

package pulid

import (
	"sync"
	"testing"
)

// Run with -race: a shared, unlocked entropy source fails here under concurrent creates.
func TestMustNewConcurrent(t *testing.T) {
	var mu sync.Mutex
	seen := map[ID]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5000; j++ {
				id := MustNew("AS")
				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate id %s", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

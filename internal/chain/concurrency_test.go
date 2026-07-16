package chain

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Concurrent writers are serialized by Driver.mu, so no block is lost to a
// read-modify-write race. Run under -race to also catch the d.seq / d.current
// data races that an unsynchronized Driver would exhibit.
func TestProduceBlockConcurrent(t *testing.T) {
	d := NewDriver(1)

	const goroutines = 8
	const perGoroutine = 50

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				d.ProduceBlock(nil)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, uint64(goroutines*perGoroutine), d.Snapshot().Height(),
		"blocks lost to unsynchronized writers")
}

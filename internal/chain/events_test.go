package chain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recvHead reads one head with a timeout so a missing notification fails the
// test instead of hanging it.
func recvHead(t *testing.T, ch <-chan *Block) *Block {
	t.Helper()
	select {
	case b, ok := <-ch:
		require.True(t, ok, "subscription channel was closed unexpectedly")
		return b
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a head notification")
		return nil
	}
}

func assertNoHead(t *testing.T, ch <-chan *Block) {
	t.Helper()
	select {
	case b, ok := <-ch:
		t.Fatalf("unexpected head notification (block=%v, open=%v)", b, ok)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSubscribeHeadsOnProduceBlock(t *testing.T) {
	d := NewDriver(1)
	heads, unsub := d.SubscribeHeads()
	defer unsub()

	want := d.ProduceBlock(nil)

	got := recvHead(t, heads)
	require.NotNil(t, got)
	assert.Equal(t, want.Hash, got.Hash)
	assert.Equal(t, uint64(1), got.Number)
}

// The notification must be published after the new snapshot, so a client that
// queries the chain on receiving a head already sees that block.
func TestSubscribeHeadsFiresAfterSnapshotIsVisible(t *testing.T) {
	d := NewDriver(1)
	heads, unsub := d.SubscribeHeads()
	defer unsub()

	d.ProduceBlock(nil)
	got := recvHead(t, heads)

	snap := d.Snapshot()
	assert.Equal(t, got.Hash, snap.Head().Hash, "head notification arrived before the snapshot was published")
	require.NotNil(t, snap.ByHash(got.Hash))
}

// A reorg publishes the new branch tip. Its ParentHash is one the client has
// never seen, which is how a reorg-aware client detects the reorg.
func TestSubscribeHeadsOnReorg(t *testing.T) {
	d := NewDriver(1)
	heads, unsub := d.SubscribeHeads()
	defer unsub()

	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
		recvHead(t, heads) // drain heights 1..3
	}
	oldHead := d.Snapshot().Head().Hash

	require.NoError(t, d.Reorg(1, make([][]Tx, 4))) // new head at height 5

	got := recvHead(t, heads)
	require.NotNil(t, got)
	assert.Equal(t, uint64(5), got.Number)
	assert.NotEqual(t, oldHead, got.Hash)
	assert.Equal(t, d.Snapshot().Head().Hash, got.Hash)
}

// A rejected write publishes nothing: the chain did not change.
func TestSubscribeHeadsSilentOnRejectedReorg(t *testing.T) {
	d := NewDriver(1)
	for i := 0; i < 3; i++ {
		d.ProduceBlock(nil)
	}
	heads, unsub := d.SubscribeHeads()
	defer unsub()

	require.Error(t, d.Reorg(2, make([][]Tx, 1))) // too short -> rejected
	assertNoHead(t, heads)
}

func TestUnsubscribeStopsNotifications(t *testing.T) {
	d := NewDriver(1)
	heads, unsub := d.SubscribeHeads()

	d.ProduceBlock(nil)
	recvHead(t, heads)

	unsub()
	d.ProduceBlock(nil)

	// After unsubscribing the channel yields nothing (it may be closed, which
	// reads as a zero value — either way no live head arrives).
	select {
	case b, ok := <-heads:
		assert.False(t, ok, "expected no live head after unsubscribe, got %v", b)
	case <-time.After(50 * time.Millisecond):
	}
}

// Unsubscribing twice must not panic (double close) — callers often defer it.
func TestUnsubscribeIsIdempotent(t *testing.T) {
	d := NewDriver(1)
	_, unsub := d.SubscribeHeads()
	unsub()
	assert.NotPanics(t, unsub)
}

func TestSubscribeHeadsFanOut(t *testing.T) {
	d := NewDriver(1)
	a, unsubA := d.SubscribeHeads()
	defer unsubA()
	b, unsubB := d.SubscribeHeads()
	defer unsubB()

	want := d.ProduceBlock(nil)

	assert.Equal(t, want.Hash, recvHead(t, a).Hash)
	assert.Equal(t, want.Hash, recvHead(t, b).Hash)
}

// A subscriber that never drains must not block the writer: the driver keeps
// producing, and the slow subscriber is dropped (its channel closed) rather
// than silently missing heads.
func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	d := NewDriver(1)
	heads, unsub := d.SubscribeHeads()
	defer unsub()

	// Never read from heads. Produce far more blocks than any sane buffer.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			d.ProduceBlock(nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ProduceBlock blocked on a slow subscriber")
	}

	assert.Equal(t, uint64(5000), d.Snapshot().Height(), "chain must keep advancing")

	// Drain: the channel must end up closed rather than delivering forever.
	closed := false
	deadline := time.After(2 * time.Second)
drain:
	for range 10000 {
		select {
		case _, ok := <-heads:
			if !ok {
				closed = true
				break drain
			}
		case <-deadline:
			break drain
		}
	}
	assert.True(t, closed, "slow subscriber should have been dropped (channel closed)")
}

package chain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A chain whose blocks all claim the same moment breaks any client that reads
// block time: "ignore anything older than a day" skips everything, and a
// time-to-confirmation estimate divides by zero. The value is derived from the
// height rather than the wall clock, so the same scenario replays identically.
func TestBlockTimestampsFollowTheHeight(t *testing.T) {
	const genesis, interval = 1735689600, 12

	d := NewDriver(1, WithGenesisTime(genesis), WithBlockInterval(interval))

	assert.Equal(t, uint64(genesis), d.Snapshot().Head().Timestamp,
		"genesis carries the start of the chain")

	for n := uint64(1); n <= 5; n++ {
		b := d.ProduceBlock(nil)
		assert.Equalf(t, genesis+n*interval, b.Timestamp, "block %d", n)
	}
}

func TestBlockTimestampsStrictlyIncrease(t *testing.T) {
	d := NewDriver(1)

	prev := d.Snapshot().Head().Timestamp
	for n := 0; n < 10; n++ {
		b := d.ProduceBlock(nil)
		assert.Greaterf(t, b.Timestamp, prev, "block %d should be later than its parent", b.Number)
		prev = b.Timestamp
	}
}

// Left alone, the chain should look like mainnet rather than like 1970, since
// that is what a client's own thresholds are usually written against.
func TestDefaultClockIsRealistic(t *testing.T) {
	d := NewDriver(1)

	first := d.ProduceBlock(nil)
	second := d.ProduceBlock(nil)

	assert.Equal(t, uint64(12), second.Timestamp-first.Timestamp,
		"the default interval should match mainnet")
	assert.Greater(t, first.Timestamp, uint64(1_700_000_000),
		"the default chain should not start at the epoch")
}

// The engine drives the chain much faster than the chain claims to run, so a
// scenario can cover minutes of block time in milliseconds of wall clock.
// That only works if the timestamp never reads a real clock.
func TestTimestampsDoNotDependOnWallClock(t *testing.T) {
	first := NewDriver(1)
	second := NewDriver(1)

	for n := 0; n < 5; n++ {
		a := first.ProduceBlock(nil)
		b := second.ProduceBlock(nil)
		require.Equal(t, a.Timestamp, b.Timestamp, "two runs must agree at height %d", a.Number)
	}
}

// A reorg replaces the blocks above the fork point, and the replacements are
// timed by the height they occupy rather than by when they were built.
func TestReorgBranchIsTimedByHeight(t *testing.T) {
	d := NewDriver(1)
	for n := 0; n < 4; n++ {
		d.ProduceBlock(nil)
	}
	before := d.Snapshot().ByNumber(3)

	require.NoError(t, d.Reorg(2, make([][]Tx, 4)))

	after := d.Snapshot().ByNumber(3)
	require.NotEqual(t, before.Hash, after.Hash, "the reorg should have replaced height 3")
	assert.Equal(t, before.Timestamp, after.Timestamp,
		"a block's time comes from its height, so the replacement matches the orphan")

	head := d.Snapshot().Head()
	assert.Equal(t, uint64(6), head.Number)
	assert.Greater(t, head.Timestamp, after.Timestamp, "the new tip is later than height 3")
}

// The timestamp is a function of the height, which hashBlock already covers,
// so folding it in would add no information while changing every hash ever
// recorded. This pins that decision: seq, not the clock, is what keeps a
// branch block distinct from the orphan it replaces.
func TestClockDoesNotChangeBlockHashes(t *testing.T) {
	plain := NewDriver(1)
	shifted := NewDriver(1, WithGenesisTime(2_000_000_000), WithBlockInterval(600))

	for n := 0; n < 4; n++ {
		a := plain.ProduceBlock(nil)
		b := shifted.ProduceBlock(nil)
		require.Equal(t, a.Hash, b.Hash, "height %d hashed differently under a different clock", a.Number)
		require.NotEqual(t, a.Timestamp, b.Timestamp, "the clocks should differ for this to mean anything")
	}
}

// A zero interval would freeze the clock and reintroduce the bug this change
// exists to fix. NewDriver cannot report an error, so it keeps its default;
// scenario files are where a zero is rejected out loud.
func TestAZeroIntervalDoesNotFreezeTheClock(t *testing.T) {
	d := NewDriver(1, WithBlockInterval(0))

	first := d.ProduceBlock(nil)
	second := d.ProduceBlock(nil)
	assert.Greater(t, second.Timestamp, first.Timestamp)
}

// The epoch is a legitimate choice, unlike a zero interval — a scenario may
// want its chain to start there.
func TestGenesisTimeMayBeZero(t *testing.T) {
	d := NewDriver(1, WithGenesisTime(0))

	assert.Equal(t, uint64(0), d.Snapshot().Head().Timestamp)
	assert.Equal(t, uint64(12), d.ProduceBlock(nil).Timestamp)
}

package scenario

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scenario that fails to describe the run it claims to describe is the worst
// outcome for a chaos tool, so every rejection has to say what is wrong and
// where. Each case asserts on fragments a human would need to fix the file.
func TestValidationRejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string // substrings the error must contain
	}{
		{
			name: "missing name",
			yaml: `
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]`,
			want: []string{"name"},
		},
		{
			name: "empty timeline",
			yaml: `
name: nothing
timeline: []`,
			want: []string{"timeline", "empty"},
		},
		{
			name: "event with no action",
			yaml: `
name: s
timeline:
  - at_height: 1`,
			want: []string{"timeline[0]", "no action", "produce"},
		},
		{
			name: "event with two actions",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]
    finalize:
      height: 1`,
			want: []string{"timeline[0]", "produce", "finalize"},
		},
		{
			name: "at_height zero is genesis and cannot host an event",
			yaml: `
name: s
timeline:
  - at_height: 0
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]`,
			want: []string{"timeline[0]", "at_height"},
		},
		{
			name: "timeline out of order",
			yaml: `
name: s
timeline:
  - at_height: 5
    finalize: {height: 1}
  - at_height: 3
    finalize: {height: 1}`,
			want: []string{"timeline[1]", "at_height", "order"},
		},
		{
			name: "produce with no txs",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: []`,
			want: []string{"timeline[0]", "produce", "txs"},
		},
		{
			name: "tx missing id",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{from: a, to: b, value: "1"}]`,
			want: []string{"timeline[0]", "txs[0]", "id"},
		},
		{
			name: "duplicate tx id",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: dup, from: a, to: b, value: "1"}]
  - at_height: 2
    produce:
      txs: [{id: dup, from: a, to: b, value: "1"}]`,
			want: []string{"dup", "duplicate"},
		},
		{
			name: "tx value is not a number",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "lots"}]`,
			want: []string{"timeline[0]", "value", "lots"},
		},
		{
			name: "tx value is negative",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "-5"}]`,
			want: []string{"value", "negative"},
		},
		{
			name: "reorg branch does not outgrow the chain",
			yaml: `
name: s
timeline:
  - at_height: 6
    reorg:
      fork_from: 4
      branch_length: 2`,
			want: []string{"timeline[0]", "reorg", "outgrow"},
		},
		{
			name: "reorg forks above the current height",
			yaml: `
name: s
timeline:
  - at_height: 3
    reorg:
      fork_from: 5
      branch_length: 4`,
			want: []string{"fork_from", "5"},
		},
		{
			name: "reorg branch length must be positive",
			yaml: `
name: s
timeline:
  - at_height: 3
    reorg:
      fork_from: 1
      branch_length: 0`,
			want: []string{"branch_length"},
		},
		{
			name: "reorg branch tx sits outside the new branch",
			yaml: `
name: s
timeline:
  - at_height: 2
    produce:
      txs: [{id: dep, from: a, to: b, value: "1"}]
  - at_height: 4
    reorg:
      fork_from: 3
      branch_length: 3
      txs:
        - at_height: 9
          ids: [dep]`,
			want: []string{"at_height", "9", "branch"},
		},
		{
			name: "reorg references an unknown tx id",
			yaml: `
name: s
timeline:
  - at_height: 4
    reorg:
      fork_from: 2
      branch_length: 4
      txs:
        - at_height: 3
          ids: [ghost]`,
			want: []string{"ghost", "unknown"},
		},
		{
			// A reorg leaves the chain at the tip of its branch, so the second
			// one here has to outgrow 9, not the 8 it is written against.
			// Accepting it would enable a reorg that loses the fork choice and
			// therefore never happens — a green run against nothing.
			name: "second reorg does not outgrow the branch the first one left",
			yaml: `
name: s
timeline:
  - at_height: 8
    reorg:
      fork_from: 4
      branch_length: 5
  - at_height: 8
    reorg:
      fork_from: 7
      branch_length: 2`,
			want: []string{"timeline[1]", "outgrow", "9"},
		},
		{
			name: "produce targets a height a reorg already carried the chain past",
			yaml: `
name: s
timeline:
  - at_height: 8
    reorg:
      fork_from: 4
      branch_length: 5
  - at_height: 9
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]`,
			want: []string{"timeline[1]", "produce", "9"},
		},
		{
			name: "two produce events at the same height",
			yaml: `
name: s
timeline:
  - at_height: 3
    produce:
      txs: [{id: a1, from: a, to: b, value: "1"}]
  - at_height: 3
    produce:
      txs: [{id: a2, from: a, to: b, value: "1"}]`,
			want: []string{"timeline[1]", "already stands at 3"},
		},
		{
			name: "end_at_height below where a reorg leaves the chain",
			yaml: `
name: s
end_at_height: 8
timeline:
  - at_height: 8
    reorg:
      fork_from: 4
      branch_length: 5`,
			want: []string{"end_at_height", "8", "9"},
		},
		{
			name: "finalize above the current height",
			yaml: `
name: s
timeline:
  - at_height: 3
    finalize:
      height: 9`,
			want: []string{"finalize", "9"},
		},
		{
			// Driver.Reorg returns ErrReorgBelowFinalized, so this would fail
			// mid-run. It is also the shape an author reaches for when trying
			// to write "a reorg eats a finalized deposit", which finality
			// makes impossible.
			name: "reorg reaches below a finalized height",
			yaml: `
name: s
timeline:
  - at_height: 10
    finalize: {height: 9}
  - at_height: 11
    reorg:
      fork_from: 4
      branch_length: 8`,
			want: []string{"timeline[1]", "fork_from", "finalized", "9"},
		},
		{
			name: "finalize moves the watermark backwards",
			yaml: `
name: s
timeline:
  - at_height: 10
    finalize: {height: 9}
  - at_height: 11
    finalize: {height: 3}`,
			want: []string{"timeline[1]", "finalize", "3", "9"},
		},
		{
			// A fault on a method the handler does not serve is never
			// consulted, so it registers, fires nothing, and the run is green.
			name: "fault on a method that is not served",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault: {method: eth_getTransactionReciept, type: false_200}`,
			want: []string{"eth_getTransactionReciept", "not served", "eth_getTransactionReceipt"},
		},
		{
			name: "two reorg branch blocks on one height",
			yaml: `
name: s
timeline:
  - at_height: 2
    produce:
      txs: [{id: d1, from: a, to: b, value: "1"}, {id: d2, from: a, to: b, value: "1"}]
  - at_height: 4
    reorg:
      fork_from: 3
      branch_length: 3
      txs:
        - {at_height: 4, ids: [d1]}
        - {at_height: 4, ids: [d2]}`,
			want: []string{"txs[1]", "at_height", "4", "twice"},
		},
		{
			name: "one tx placed in two reorg branch blocks",
			yaml: `
name: s
timeline:
  - at_height: 2
    produce:
      txs: [{id: d1, from: a, to: b, value: "1"}]
  - at_height: 4
    reorg:
      fork_from: 3
      branch_length: 3
      txs:
        - {at_height: 4, ids: [d1]}
        - {at_height: 5, ids: [d1]}`,
			want: []string{"txs[1]", "d1", "twice"},
		},
		{
			name: "reorg branch block with an empty ids list",
			yaml: `
name: s
timeline:
  - at_height: 4
    reorg:
      fork_from: 3
      branch_length: 3
      txs:
        - {at_height: 4, ids: []}`,
			want: []string{"txs[0]", "ids", "empty"},
		},
		{
			name: "fault with unknown type",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: eth_blockNumber
      type: false500`,
			want: []string{"type", "false500"},
		},
		{
			name: "fault missing method",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      type: false_200`,
			want: []string{"method"},
		},
		{
			name: "delay fault without a delay",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: "*"
      type: delay`,
			want: []string{"delay"},
		},
		{
			name: "unparseable delay",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: "*"
      type: delay
      delay: soon`,
			want: []string{"delay", "soon"},
		},
		{
			name: "delay set on a false_200 fault would be silently ignored",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: "*"
      type: false_200
      delay: 1s`,
			want: []string{"delay", "false_200"},
		},
		{
			name: "negative fault count",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: "*"
      type: false_200
      count: -3`,
			want: []string{"count"},
		},
		{
			name: "end_at_height before the last event",
			yaml: `
name: s
end_at_height: 2
timeline:
  - at_height: 5
    finalize: {height: 1}`,
			want: []string{"end_at_height", "2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			require.Error(t, err, "this scenario should have been rejected")
			for _, want := range c.want {
				assert.Containsf(t, err.Error(), want,
					"error should mention %q so the author can fix it; got: %v", want, err)
			}
		})
	}
}

// Shapes that look unusual but are legitimate must not be rejected.
func TestValidationAccepts(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "several events at the same height",
			yaml: `
name: s
timeline:
  - at_height: 5
    produce:
      txs: [{id: t, from: a, to: b, value: "1"}]
  - at_height: 5
    fault:
      method: "*"
      type: false_200`,
		},
		{
			// The point of sharing a height: no block is produced between the
			// reorg and the fault, so the client meets the lie in the same beat
			// it learns the chain changed under it. The fault's at_height is
			// below where the reorg left the chain, which is not an error — the
			// event simply runs immediately.
			name: "a fault at the height a reorg was written against",
			yaml: `
name: reorg-then-lie
timeline:
  - at_height: 5
    produce:
      txs: [{id: alice-deposit, from: alice, to: exchange, value: "1000000000000000000"}]
  - at_height: 8
    reorg:
      fork_from: 4
      branch_length: 5
  - at_height: 8
    fault:
      method: eth_getTransactionReceipt
      type: false_200
      count: 3
end_at_height: 15`,
		},
		{
			name: "forking from the current head",
			yaml: `
name: s
timeline:
  - at_height: 4
    reorg:
      fork_from: 4
      branch_length: 1`,
		},
		{
			// Driver.Reorg refuses fork_from below the watermark, not at it,
			// so the deepest legal reorg is the interesting boundary.
			name: "forking exactly at the finalized watermark",
			yaml: `
name: s
timeline:
  - at_height: 10
    finalize: {height: 6}
  - at_height: 11
    reorg:
      fork_from: 6
      branch_length: 6`,
		},
		{
			name: "finalizing the same height twice",
			yaml: `
name: s
timeline:
  - at_height: 10
    finalize: {height: 8}
  - at_height: 12
    finalize: {height: 8}`,
		},
		{
			name: "finalizing the current head",
			yaml: `
name: s
timeline:
  - at_height: 4
    finalize:
      height: 4`,
		},
		{
			name: "literal hex addresses instead of symbolic names",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs:
        - id: t
          from: "0xaaaa1111bbbb2222cccc3333dddd4444eeee5555"
          to: "0x9999888877776666555544443333222211110000"
          value: "0"`,
		},
		{
			name: "a very large value that would lose precision as a float",
			yaml: `
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "1000000000000000001"}]`,
		},
		{
			name: "unlimited fault with no count",
			yaml: `
name: s
timeline:
  - at_height: 1
    fault:
      method: eth_blockNumber
      type: false_200`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			assert.NoError(t, err)
		})
	}
}

// A big value must survive as an exact integer: 1 ETH + 1 wei is beyond what a
// float64 can represent, which is why values are strings in the schema.
func TestLargeValueKeepsFullPrecision(t *testing.T) {
	s, err := Parse([]byte(`
name: s
timeline:
  - at_height: 1
    produce:
      txs: [{id: t, from: a, to: b, value: "1000000000000000001"}]`))
	require.NoError(t, err)
	assert.Equal(t, "1000000000000000001", s.Timeline[0].Produce.Txs[0].Value)
}

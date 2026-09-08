package scenario

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const flagship = `
name: reorg-eats-a-deposit
description: A confirmed deposit is orphaned by a longer competing branch
chain_id: 1337
end_at_height: 12

timeline:
  - at_height: 5
    produce:
      txs:
        - id: alice-deposit
          from: alice
          to: exchange
          value: "1000000000"

  - at_height: 8
    reorg:
      fork_from: 4
      branch_length: 5

  - at_height: 10
    finalize:
      height: 9

  - at_height: 11
    fault:
      method: eth_getTransactionReceipt
      type: false_200
      count: 1

  - at_height: 12
    clear_faults: {}
`

func TestParseFlagshipScenario(t *testing.T) {
	s, err := Parse([]byte(flagship))
	require.NoError(t, err)

	assert.Equal(t, "reorg-eats-a-deposit", s.Name)
	assert.Equal(t, uint64(1337), s.ChainID)
	assert.Equal(t, uint64(12), s.EndAtHeight)
	require.Len(t, s.Timeline, 5)

	// produce
	e := s.Timeline[0]
	assert.Equal(t, uint64(5), e.AtHeight)
	require.NotNil(t, e.Produce)
	require.Len(t, e.Produce.Txs, 1)
	assert.Equal(t, "alice-deposit", e.Produce.Txs[0].ID)
	assert.Equal(t, "alice", e.Produce.Txs[0].From)
	assert.Equal(t, "exchange", e.Produce.Txs[0].To)
	assert.Equal(t, "1000000000", e.Produce.Txs[0].Value)
	assert.Nil(t, e.Reorg, "an event carries exactly one action")

	// reorg
	e = s.Timeline[1]
	require.NotNil(t, e.Reorg)
	assert.Equal(t, uint64(4), e.Reorg.ForkFrom)
	assert.Equal(t, 5, e.Reorg.BranchLength)
	assert.Empty(t, e.Reorg.Txs, "a branch with no txs is a branch of empty blocks")

	// finalize
	require.NotNil(t, s.Timeline[2].Finalize)
	assert.Equal(t, uint64(9), s.Timeline[2].Finalize.Height)

	// fault
	f := s.Timeline[3].Fault
	require.NotNil(t, f)
	assert.Equal(t, "eth_getTransactionReceipt", f.Method)
	assert.Equal(t, "false_200", f.Type)
	assert.Equal(t, 1, f.Count)

	require.NotNil(t, s.Timeline[4].ClearFaults)
}

// chain_id defaults to 1 so a scenario need not spell it out.
func TestParseDefaultsChainID(t *testing.T) {
	s, err := Parse([]byte(`
name: minimal
timeline:
  - at_height: 1
    produce:
      txs: [{id: t1, from: a, to: b, value: "1"}]
`))
	require.NoError(t, err)
	assert.Equal(t, uint64(1), s.ChainID)
}

// A reorg branch may place previously defined txs into specific new blocks.
func TestParseReorgWithBranchTxs(t *testing.T) {
	s, err := Parse([]byte(`
name: reinclude
timeline:
  - at_height: 3
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "10"}]
  - at_height: 5
    reorg:
      fork_from: 2
      branch_length: 4
      txs:
        - at_height: 4
          ids: [dep]
`))
	require.NoError(t, err)

	r := s.Timeline[1].Reorg
	require.NotNil(t, r)
	require.Len(t, r.Txs, 1)
	assert.Equal(t, uint64(4), r.Txs[0].AtHeight)
	assert.Equal(t, []string{"dep"}, r.Txs[0].IDs)
}

func TestParseDelayFault(t *testing.T) {
	s, err := Parse([]byte(`
name: slow
timeline:
  - at_height: 1
    fault:
      method: "*"
      type: delay
      delay: 250ms
`))
	require.NoError(t, err)

	f := s.Timeline[0].Fault
	require.NotNil(t, f)
	assert.Equal(t, "*", f.Method)
	assert.Equal(t, "delay", f.Type)
	assert.Equal(t, "250ms", f.Delay)
	assert.Equal(t, 0, f.Count, "an omitted count means unlimited")
}

// Malformed YAML must fail as a parse problem, not as a validation one.
func TestParseRejectsMalformedYAML(t *testing.T) {
	_, err := Parse([]byte("name: [unclosed\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "yaml")
}

func TestLoadReadsAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	require.NoError(t, os.WriteFile(path, []byte(flagship), 0o600))

	s, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "reorg-eats-a-deposit", s.Name)
}

func TestLoadReportsAMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope.yaml", "the error should name the file")
}

// A mistyped field is rejected rather than dropped. Left to yaml's default an
// unknown key vanishes, so "producee:" would yield an event with no action and
// the scenario would run green having produced nothing.
func TestParseRejectsUnknownFields(t *testing.T) {
	cases := map[string]string{
		"mistyped action": `
name: oops
timeline:
  - at_height: 5
    producee:
      txs: [{id: t, from: a, to: b, value: "1"}]`,

		"mistyped action field": `
name: oops
timeline:
  - at_height: 5
    reorg:
      fork_frm: 4
      branch_length: 5`,

		"mistyped top-level field": `
name: oops
chainid: 7
timeline:
  - at_height: 5
    finalize: {height: 1}`,

		"stray field on a tx": `
name: oops
timeline:
  - at_height: 5
    produce:
      txs: [{id: t, from: a, to: b, value: "1", note: "why is this here"}]`,
	}

	for label, doc := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := Parse([]byte(doc))
			require.Error(t, err, "an unknown field should not be silently dropped")
			assert.Contains(t, err.Error(), "yaml", "the author should see this is a file problem")
		})
	}
}

// Comments are the way to annotate a scenario, and must still parse.
func TestParseAllowsComments(t *testing.T) {
	s, err := Parse([]byte(`
# The deposit is orphaned by a longer branch that omits it.
name: commented
timeline:
  - at_height: 5      # the deposit lands here
    produce:
      txs: [{id: dep, from: alice, to: exchange, value: "1"}]`))
	require.NoError(t, err)
	assert.Equal(t, "commented", s.Name)
}

func TestParseRejectsAnEmptyFile(t *testing.T) {
	_, err := Parse(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

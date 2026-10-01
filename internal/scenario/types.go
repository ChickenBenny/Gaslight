package scenario

// Scenario is a parsed scenario file: the script Gaslight performs. Everything
// here is the wire shape, held as written in the file — values are strings so
// that parsing and validation happen explicitly and can report what is wrong.
type Scenario struct {
	Name             string  `yaml:"name"`
	Description      string  `yaml:"description,omitempty"`
	ChainID          uint64  `yaml:"chain_id,omitempty"`
	GenesisTimestamp *uint64 `yaml:"genesis_timestamp,omitempty"`
	BlockInterval    *uint64 `yaml:"block_interval,omitempty"`
	EndAtHeight      uint64  `yaml:"end_at_height,omitempty"`
	Timeline         []Event `yaml:"timeline"`
}

// Event is one beat of the timeline: at a given height, exactly one action.
// Actions are pointers so that a nil means "not given" — an action with no
// fields still needs an explicit "{}" in the file.
type Event struct {
	AtHeight    uint64          `yaml:"at_height"`
	Produce     *ProduceAction  `yaml:"produce,omitempty"`
	Reorg       *ReorgAction    `yaml:"reorg,omitempty"`
	Finalize    *FinalizeAction `yaml:"finalize,omitempty"`
	Fault       *FaultAction    `yaml:"fault,omitempty"`
	ClearFaults *ClearAction    `yaml:"clear_faults,omitempty"`
}

// ProduceAction appends a block carrying these transactions.
type ProduceAction struct {
	Txs []TxSpec `yaml:"txs"`
}

// TxSpec describes one transaction. ID is a human-readable handle the rest of
// the file can refer to; From and To are either symbolic names or 0x addresses;
// Value is a base-10 string because wei amounts exceed what a float can hold.
type TxSpec struct {
	ID    string `yaml:"id"`
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Value string `yaml:"value"`
}

// ReorgAction replaces the chain above ForkFrom with BranchLength new blocks.
// The branch is empty unless Txs places previously defined transactions into
// specific new blocks — so omitting Txs is how a transaction gets orphaned for
// good.
type ReorgAction struct {
	ForkFrom     uint64        `yaml:"fork_from"`
	BranchLength int           `yaml:"branch_length"`
	Txs          []BranchBlock `yaml:"txs,omitempty"`
}

// BranchBlock puts transactions into one block of a reorg branch, addressed by
// absolute height like the rest of the file.
type BranchBlock struct {
	AtHeight uint64   `yaml:"at_height"`
	IDs      []string `yaml:"ids"`
}

// FinalizeAction moves the finalized watermark, below which no reorg may reach.
type FinalizeAction struct {
	Height uint64 `yaml:"height"`
}

// FaultAction enables one fault. Type and Delay stay as written and are
// validated with faults.ParseType and time.ParseDuration, so a typo is a parse
// error rather than a fault that silently never fires.
type FaultAction struct {
	Method string `yaml:"method"`
	Type   string `yaml:"type"`
	Count  int    `yaml:"count,omitempty"`
	Delay  string `yaml:"delay,omitempty"`
}

// ClearAction removes every fault in effect. It carries no fields, so a file
// must spell it as "clear_faults: {}".
type ClearAction struct{}

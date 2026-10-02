package goldentest

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Named for what it does rather than for how often it is needed. Rewriting a
// recording is how a reproducibility bug gets mistaken for a stale fixture, so
// the flag should read like a decision and not like routine maintenance.
var rewrite = flag.Bool("rewrite-golden", false,
	"replace the stored recordings with what the code produces now")

const examplesDir = "../../examples"

// replays is high because the cheapest way to be nondeterministic is to range
// over a small map, and Go randomises that far less than it looks. Measured:
// two iterations of a three-key map agree about 76% of the time, so comparing
// only two runs would miss such a bug three times in four. At 32 runs the
// chance of missing it falls to roughly 1 in 5000, and a run costs
// microseconds.
const replays = 32

func goldenPath(scenarioPath string) string {
	name := strings.TrimSuffix(filepath.Base(scenarioPath), filepath.Ext(scenarioPath))
	return filepath.Join("testdata", name+".golden")
}

func examples(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(examplesDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "there should be scenarios to record")
	return paths
}

// The first of the two checks, and the one that cannot be argued with: the
// same file, played many times in the same test, has to produce the same chain
// every time.
//
// It needs no stored recording, so there is nothing to rewrite when it fails.
// That matters, because the other check reports randomness as an intermittent
// mismatch — indistinguishable from a stale recording, and therefore likely to
// be "fixed" by rewriting until someone disables it.
func TestTheSameScenarioAlwaysPlaysTheSameWay(t *testing.T) {
	for _, path := range examples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			first, err := Record(path)
			require.NoError(t, err)

			for i := 2; i <= replays; i++ {
				again, err := Record(path)
				require.NoError(t, err)
				if again == first {
					continue
				}
				t.Fatalf("%s played differently on run %d of %d.\n\n%s\n\n"+
					"This is nondeterminism, not a stale recording \u2014 rewriting the golden "+
					"file will not help, and the mismatch will come back. Look for a map being "+
					"ranged over, a clock being read, or an address being printed.",
					filepath.Base(path), i, replays, firstDifference(first, again))
			}
		})
	}
}

// The second check: what the code produces now against what it produced when
// the recording was made. This is the one that catches a change that is
// perfectly deterministic and still wrong — a reordered hash input, a different
// address prefix, a timestamp folded into a block hash.
func TestScenariosStillProduceTheRecordedChain(t *testing.T) {
	for _, path := range examples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			got, err := Record(path)
			require.NoError(t, err)

			golden := goldenPath(path)
			if *rewrite {
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o750))
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o600))
				t.Logf("rewrote %s", golden)
				return
			}

			want, err := os.ReadFile(golden)
			if os.IsNotExist(err) {
				t.Fatalf("%s has no recording yet.\n\nCreate it with:\n"+
					"    go test ./internal/goldentest -rewrite-golden\n\n"+
					"then read the diff before committing it.", golden)
			}
			require.NoError(t, err)

			if string(want) == got {
				return
			}
			t.Fatalf("%s no longer produces the chain it was recorded with.\n\n%s\n\n"+
				"Nothing about a scenario file should change what it produces unless you "+
				"meant it to. If you did, read the whole diff first — every hash changing "+
				"means the hashing rules moved, while one step onward changing means that "+
				"step's behaviour did.\n\n"+
				"To accept:\n    go test ./internal/goldentest -rewrite-golden",
				filepath.Base(path), firstDifference(string(want), got))
		})
	}
}

// Every example is recorded, so adding one without a recording fails rather
// than being quietly left out of the gate.
func TestEveryExampleIsRecorded(t *testing.T) {
	if *rewrite {
		t.Skip("recordings are being written")
	}
	for _, path := range examples(t) {
		_, err := os.Stat(goldenPath(path))
		require.NoErrorf(t, err, "%s is not covered by the reproducibility gate", filepath.Base(path))
	}
}

// firstDifference reports the first line that disagrees with context, because
// which line it is carries most of the diagnosis.
func firstDifference(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := lineAt(a, i), lineAt(b, i)
		if x == y {
			continue
		}
		var out strings.Builder
		fmt.Fprintf(&out, "first difference at line %d:\n", i+1)
		if i > 0 {
			fmt.Fprintf(&out, "      %s\n", lineAt(a, i-1))
		}
		fmt.Fprintf(&out, "  -   %s\n  +   %s\n", x, y)
		fmt.Fprintf(&out, "\n%d of %d lines differ", countDiffering(a, b), max(len(a), len(b)))
		return out.String()
	}
	return "the recordings are equal line by line but differ in length"
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(no such line)"
}

func countDiffering(a, b []string) int {
	n := 0
	for i := 0; i < len(a) || i < len(b); i++ {
		if lineAt(a, i) != lineAt(b, i) {
			n++
		}
	}
	return n
}

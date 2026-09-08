package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse reads a scenario and rejects anything that would not perform the run it
// describes.
//
// Unknown fields are errors rather than being ignored: yaml drops keys it does
// not recognise, so a mistyped "producee:" would otherwise parse into an event
// with no action at all, and the scenario would run green having done nothing.
// A comment belongs in a "#" line, not in a stray field.
func Parse(data []byte) (*Scenario, error) {
	var s Scenario

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("scenario: yaml: the file is empty")
		}
		// The yaml package prefixes its own errors, so this only adds ours.
		return nil, fmt.Errorf("scenario: %w", err)
	}

	if s.ChainID == 0 {
		s.ChainID = 1
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scenario: read %s: %w", path, err)
	}
	// Parse already reports where in the file the problem is, so wrapping it
	// again would only repeat the "scenario:" prefix.
	return Parse(data)
}

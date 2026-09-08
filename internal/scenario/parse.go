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
// Anything the file says that would not take effect is an error rather than
// being ignored, because a scenario that quietly does less than it says is a
// green run against nothing. So unknown fields are rejected — yaml drops keys
// it does not recognise, and a mistyped "producee:" would otherwise parse into
// an event with no action at all — and so is a second yaml document, which
// Decode would leave unread. A comment belongs in a "#" line, not in a stray
// field or a trailing document.
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

	var extra Scenario
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		// The only acceptable outcome: there was nothing after the first
		// document.
	case err != nil:
		return nil, fmt.Errorf("scenario: %w", err)
	default:
		return nil, fmt.Errorf("scenario: yaml: the file holds more than one document and only the "+
			"first would run (found %q after a \"---\" separator)", extra.Name)
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

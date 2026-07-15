// Package diagnostics implements the CI ratchet: the set of known
// generation degradations is committed as a baseline, regeneration fails on
// any NEW degradation, and fixing degradations shrinks the baseline.
package diagnostics

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// baselineFile is the committed baseline shape.
type baselineFile struct {
	// Entries is the sorted, deduplicated diagnostic set.
	Entries []string `json:"entries"`
}

// normalize sorts and deduplicates diagnostics.
func normalize(diagnostics []string) []string {
	set := make(map[string]bool, len(diagnostics))
	for _, entry := range diagnostics {
		set[entry] = true
	}
	entries := make([]string, 0, len(set))
	for entry := range set {
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return entries
}

// WriteBaseline writes the normalized diagnostic set to path.
func WriteBaseline(path string, diagnostics []string) error {
	data, err := json.MarshalIndent(baselineFile{Entries: normalize(diagnostics)}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// CheckBaseline returns the diagnostics not present in the committed
// baseline (the ratchet violations).
func CheckBaseline(path string, diagnostics []string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("diagnostics baseline: %w", err)
	}
	var baseline baselineFile
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("diagnostics baseline %s: %w", path, err)
	}
	known := make(map[string]bool, len(baseline.Entries))
	for _, entry := range baseline.Entries {
		known[entry] = true
	}
	var newEntries []string
	for _, entry := range normalize(diagnostics) {
		if !known[entry] {
			newEntries = append(newEntries, entry)
		}
	}
	return newEntries, nil
}

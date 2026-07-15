package pipeline

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ReadRootsFile reads the committed emit-roots list: one root namespace per
// line, blank lines and '#' comments ignored. The file is the single pinned
// definition of the committed generated surface — cmd/generate falls back to
// it when --namespace is not given, so CI, the winmd-update workflow, and
// local regeneration all agree on the root set.
func ReadRootsFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var roots []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			roots = append(roots, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read roots file %s: %w", path, err)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("roots file %s lists no namespaces", path)
	}
	return roots, nil
}

// Package manifest reads and extends kybernes.yaml without a YAML dependency.
// ponytail: line-based; assumes top-level scalars plus a trailing `features:` list.
// Swap in a YAML library when the manifest grows nested keys.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const File = "kybernes.yaml"

// Read returns the top-level `key: value` pairs.
func Read(dir string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil, fmt.Errorf("%w (run inside a Kybernes project, or pass --dir)", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ": "); ok && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "-") {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m, nil
}

// AddFeature appends name to the trailing `features:` list, creating it if absent.
func AddFeature(dir, name string) error {
	p := filepath.Join(dir, File)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	s := strings.TrimRight(string(b), "\n") + "\n"
	if !strings.Contains(s, "\nfeatures:") {
		s += "features:\n"
	}
	return os.WriteFile(p, []byte(s+"  - "+name+"\n"), 0o644)
}

// Features returns the names in the `features:` list.
func Features(dir string) []string {
	b, _ := os.ReadFile(filepath.Join(dir, File))
	var out []string
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case l == "features:":
			in = true
		case in && strings.HasPrefix(l, "  - "):
			out = append(out, strings.TrimPrefix(l, "  - "))
		default:
			in = false
		}
	}
	return out
}

// Set replaces the value of an existing top-level scalar key.
func Set(dir, key, value string) error {
	p := filepath.Join(dir, File)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, key+": ") {
			lines[i] = key + ": " + value
		}
	}
	return os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
}

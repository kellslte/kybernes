// Package lock records the hash of every generated file in kybernes.lock, so
// `update` can tell files you edited from files it may safely overwrite.
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const File = "kybernes.lock"

// Skip reports files that are never tracked: you own them (deps, manifest) or they are the lock.
func Skip(rel string) bool {
	return rel == "go.mod" || rel == "go.sum" || rel == "kybernes.yaml" || rel == File
}

func Hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Read returns path -> hash ("" map if no lock exists).
func Read(dir string) map[string]string {
	m := map[string]string{}
	b, _ := os.ReadFile(filepath.Join(dir, File))
	for _, l := range strings.Split(string(b), "\n") {
		if h, p, ok := strings.Cut(l, "  "); ok {
			m[p] = h
		}
	}
	return m
}

func Write(dir string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(m[k] + "  " + k + "\n")
	}
	return os.WriteFile(filepath.Join(dir, File), []byte(sb.String()), 0o644)
}

// Record hashes the current content of the given files (slash paths relative to dir) into the lock.
func Record(dir string, paths []string) error {
	m := Read(dir)
	for _, p := range paths {
		if Skip(p) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return err
		}
		m[p] = Hash(b)
	}
	return Write(dir, m)
}

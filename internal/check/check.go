// Package check enforces each architecture's import rules by parsing imports
// only (no build needed). Rules are edges between top-level packages under internal/.
//
// ponytail: only edges between known layers are checked; imports of packages the
// rules don't mention (e.g. internal/platform) are allowed. Add a rules file in
// kybernes.yaml if teams need custom layers.
package check

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kybernes/kybernes/internal/manifest"
)

// rules[arch][layer] = layers that layer may import (itself is always allowed).
var rules = map[string]map[string][]string{
	"layered":   {"repository": nil, "service": {"repository"}, "handler": {"service"}, "router": {"handler"}, "config": nil},
	"hexagonal": {"domain": nil, "app": {"domain"}, "store": nil, "handler": {"app"}, "router": {"handler"}, "config": nil},
	// Module-to-module access is limited to public packages by Go's internal/ rule.
	"modular": {"modules": nil, "handler": {"modules"}, "router": {"handler"}, "config": nil},
}

// Run returns one message per violated import rule in the project at dir.
func Run(dir string) ([]string, error) {
	m, err := manifest.Read(dir)
	if err != nil {
		return nil, err
	}
	rs, ok := rules[m["architecture"]]
	if !ok {
		return nil, fmt.Errorf("no rules for architecture %q", m["architecture"])
	}
	prefix := m["module"] + "/internal/"
	root := filepath.Join(dir, "internal")
	var out []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		layer, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		allowed, known := rs[layer]
		if !known {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			rest, ok := strings.CutPrefix(path, prefix)
			if !ok {
				continue
			}
			target, _, _ := strings.Cut(rest, "/")
			if _, known := rs[target]; known && target != layer && !slices.Contains(allowed, target) {
				list := strings.Join(allowed, ", ")
				if list == "" {
					list = "nothing from this project"
				}
				out = append(out, fmt.Sprintf("internal/%s: %s must not import %s (allowed: %s)",
					filepath.ToSlash(rel), layer, target, list))
			}
		}
		return nil
	})
	return out, err
}

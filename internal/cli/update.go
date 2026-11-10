package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kybernes/kybernes/internal/lock"
	"github.com/kybernes/kybernes/internal/manifest"
)

// Update re-renders the project with this binary's templates and merges the result
// using kybernes.lock (the hash of each file as last generated):
//
//	file unchanged locally           -> overwritten with the new template output
//	file edited, template unchanged  -> left alone
//	file edited AND template changed -> new output written to <file>.kybernes-new
//	file deleted locally             -> not recreated
//	file new in the template         -> created
//
// ponytail: no real 3-way merge (we keep hashes, not the old content), so edited files
// that also changed upstream need a manual merge. Store old content to upgrade this.
func Update(dir string, dry bool) ([]string, error) {
	m, err := manifest.Read(dir)
	if err != nil {
		return nil, err
	}
	o := Options{Name: m["name"], Module: m["module"], Arch: m["architecture"], Router: m["router"], DB: m["database"]}
	if o.Name == "" {
		abs, _ := filepath.Abs(dir)
		o.Name = filepath.Base(abs)
	}
	if x := strings.Trim(m["extras"], "[]"); x != "" {
		o.Extras = strings.Split(strings.ReplaceAll(x, " ", ""), ",")
	}
	var feats []string
	for _, f := range manifest.Features(dir) {
		if f != "greeting" { // starter feature belongs to the arch layer
			feats = append(feats, f)
		}
	}
	tmp, err := os.MkdirTemp("", "kybernes-update-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	paths, err := renderTo(tmp, o, feats)
	if err != nil {
		return nil, err
	}

	lk, report, changed := lock.Read(dir), []string{}, false
	seen := map[string]bool{}
	sort.Strings(paths)
	for _, rel := range paths {
		if lock.Skip(rel) || seen[rel] {
			continue
		}
		seen[rel] = true
		nb, _ := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(rel)))
		nh, dst := lock.Hash(nb), filepath.Join(dir, filepath.FromSlash(rel))
		old, tracked := lk[rel]
		cb, rerr := os.ReadFile(dst)
		write := func(p string) error {
			if dry {
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			return os.WriteFile(p, nb, 0o644)
		}
		switch {
		case rerr != nil && tracked:
			report = append(report, "skipped   "+rel+" (deleted locally)")
		case rerr != nil:
			report, changed, lk[rel] = append(report, "created   "+rel), true, nh
			if err := write(dst); err != nil {
				return nil, err
			}
		case lock.Hash(cb) == nh:
			lk[rel] = nh
		case tracked && lock.Hash(cb) == old:
			report, changed, lk[rel] = append(report, "updated   "+rel), true, nh
			if err := write(dst); err != nil {
				return nil, err
			}
		case tracked && nh == old:
			report = append(report, "kept      "+rel+" (local edits, template unchanged)")
		default:
			report, changed = append(report, "conflict  "+rel+" (new version in "+rel+".kybernes-new)"), true
			if err := write(dst + ".kybernes-new"); err != nil {
				return nil, err
			}
		}
	}
	for p := range lk {
		if !seen[p] {
			report = append(report, "orphaned  "+p+" (no longer in the template; delete if unused)")
		}
	}
	sort.Strings(report)
	if dry {
		return report, nil
	}
	if err := lock.Write(dir, lk); err != nil {
		return nil, err
	}
	if err := manifest.Set(dir, "kybernes", version); err != nil {
		return nil, err
	}
	if changed {
		if err := tidy(dir); err != nil {
			return nil, err
		}
	}
	return report, nil
}

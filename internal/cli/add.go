package cli

import (
	"fmt"
	"go/token"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/kybernes/kybernes/internal/lock"
	"github.com/kybernes/kybernes/internal/manifest"
	"github.com/kybernes/kybernes/internal/render"
	"github.com/kybernes/kybernes/templates"
)

var featRe = regexp.MustCompile(`^[a-z][a-z0-9]*$`) // doubles as a Go package name in modular

// AddFeature adds one feature using the project's architecture (see templates/feature/<arch>).
func AddFeature(dir, name string) error {
	m, err := manifest.Read(dir)
	if err != nil {
		return err
	}
	if !featRe.MatchString(name) || token.IsKeyword(name) {
		return fmt.Errorf("invalid feature name %q: lowercase letters and digits only, not a Go keyword", name)
	}
	if slices.Contains(manifest.Features(dir), name) {
		return fmt.Errorf("feature %q already exists", name)
	}
	written, err := renderFeature(dir, m["architecture"], m["module"], name)
	if err != nil {
		return err
	}
	if err := lock.Record(dir, written); err != nil {
		return err
	}
	return manifest.AddFeature(dir, name)
}

func renderFeature(dst, arch, module, name string) ([]string, error) {
	sub, err := fs.Sub(templates.FS, "feature/"+arch)
	if _, serr := fs.Stat(sub, "."); err != nil || serr != nil {
		return nil, fmt.Errorf("no feature template for architecture %q", arch)
	}
	return render.Apply(dst, sub, map[string]string{"Module": module, "Mod": name, "Title": strings.ToUpper(name[:1]) + name[1:]})
}

// AddModule is AddFeature restricted to the modular architecture, where a feature is a module.
func AddModule(dir, name string) error {
	m, err := manifest.Read(dir)
	if err != nil {
		return err
	}
	if m["architecture"] != "modular" {
		return fmt.Errorf("add module needs the modular architecture (this project is %q); use add feature", m["architecture"])
	}
	return AddFeature(dir, name)
}

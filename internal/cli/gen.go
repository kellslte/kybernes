package cli

import (
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kellslte/kybernes/internal/manifest"
	"github.com/kellslte/kybernes/internal/render"
	"github.com/kellslte/kybernes/templates"
)

// AddHandler scaffolds internal/handler/<name>.go. The template is the same for every architecture.
func AddHandler(dir, name string) ([]string, error) {
	return scaffold(dir, name, "", "gen/handler")
}

// AddService scaffolds a service where the architecture keeps business logic:
// internal/service (layered), internal/app (hexagonal), or inside module (modular).
func AddService(dir, name, module string) ([]string, error) {
	m, err := manifest.Read(dir)
	if err != nil {
		return nil, err
	}
	switch arch := m["architecture"]; {
	case arch == "modular" && module == "":
		return nil, fmt.Errorf("modular projects need --module <name> to say which module owns the service")
	case arch != "modular" && module != "":
		return nil, fmt.Errorf("--module only applies to the modular architecture")
	case arch == "modular":
		if _, err := os.Stat(filepath.Join(dir, "internal", "modules", module)); err != nil {
			return nil, fmt.Errorf("module %q does not exist (kybernes add module %s)", module, module)
		}
	}
	return scaffold(dir, name, module, "gen/service/"+m["architecture"])
}

// scaffold renders templates/<tmpl> into a temp dir first, so it can refuse to overwrite anything.
// Scaffolds are one-off starting points: they are not tracked in kybernes.lock, so `update` leaves them alone.
func scaffold(dir, name, pkg, tmpl string) ([]string, error) {
	m, err := manifest.Read(dir)
	if err != nil {
		return nil, err
	}
	if !featRe.MatchString(name) || token.IsKeyword(name) {
		return nil, fmt.Errorf("invalid name %q: lowercase letters and digits only, not a Go keyword", name)
	}
	sub, err := fs.Sub(templates.FS, tmpl)
	if _, serr := fs.Stat(sub, "."); err != nil || serr != nil {
		return nil, fmt.Errorf("no %s template for architecture %q", tmpl, m["architecture"])
	}
	tmp, err := os.MkdirTemp("", "kybernes-gen-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	data := map[string]string{"Module": m["module"], "Mod": name, "Pkg": pkg, "Title": strings.ToUpper(name[:1]) + name[1:]}
	files, err := render.Apply(tmp, sub, data)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
			return nil, fmt.Errorf("%s already exists", f)
		}
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(f)))
		dst := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return nil, err
		}
	}
	return files, nil
}

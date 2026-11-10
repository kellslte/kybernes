package cli

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"

	"github.com/kellslte/kybernes/internal/check"
	"github.com/kellslte/kybernes/templates"
)

func TestAddHandlerAndService(t *testing.T) {
	archs, _ := fs.ReadDir(templates.FS, "arch")
	for _, a := range archs {
		t.Run(a.Name(), func(t *testing.T) {
			d := t.TempDir()
			if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: a.Name(), Router: "stdlib", Dir: d}); err != nil {
				t.Fatal(err)
			}
			p, mod := d+"/demo", ""
			if a.Name() == "modular" {
				mod = "greeting"
			}
			if _, err := AddHandler(p, "invoices"); err != nil {
				t.Fatal(err)
			}
			if _, err := AddService(p, "invoices", mod); err != nil {
				t.Fatal(err)
			}
			if _, err := AddHandler(p, "invoices"); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Errorf("duplicate handler should be refused, got %v", err)
			}
			if _, err := AddService(p, "x", "nope"); err == nil && a.Name() != "modular" {
				t.Error("--module outside modular should fail")
			}
			goRun(t, p, "build", "./...")
			goRun(t, p, "vet", "./...")
			if out, _ := exec.Command("gofmt", "-l", p).Output(); len(out) > 0 {
				t.Errorf("not gofmt-clean:\n%s", out)
			}
			if bad, err := check.Run(p); err != nil || len(bad) > 0 {
				t.Errorf("check: %v %v", bad, err)
			}
			if r, err := Update(p, false); err != nil || len(r) != 0 {
				t.Errorf("update must ignore scaffolds, got %v %v", r, err)
			}
		})
	}
}

func TestAddServiceModularNeedsModule(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "modular", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddService(d+"/demo", "x", ""); err == nil {
		t.Error("modular add service without --module should fail")
	}
	if _, err := AddService(d+"/demo", "x", "missing"); err == nil {
		t.Error("unknown module should fail")
	}
}

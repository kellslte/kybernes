package cli

import (
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kellslte/kybernes/internal/check"
	"github.com/kellslte/kybernes/templates"
)

func goRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
}

// Generates every arch x router combination, adds a feature, and checks it builds and vets.
func TestMatrixBuilds(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	archs, _ := fs.ReadDir(templates.FS, "arch")
	routers, _ := fs.ReadDir(templates.FS, "router")
	for _, a := range archs {
		for _, r := range routers {
			t.Run(a.Name()+"-"+r.Name(), func(t *testing.T) {
				dir := t.TempDir()
				if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: a.Name(), Router: r.Name(), Dir: dir}); err != nil {
					t.Fatal(err)
				}
				p := dir + "/demo"
				if err := AddFeature(p, "billing"); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(p + "/kybernes.yaml")
				if !strings.HasSuffix(string(b), "features:\n  - greeting\n  - billing\n") {
					t.Errorf("manifest: %s", b)
				}
				if bad, err := check.Run(p); err != nil || len(bad) > 0 {
					t.Errorf("check on fresh project: %v %v", bad, err)
				}
				goRun(t, p, "build", "./...")
				goRun(t, p, "vet", "./...")
				if out, _ := exec.Command("gofmt", "-l", p).Output(); len(out) > 0 {
					t.Errorf("generated code not gofmt-clean:\n%s", out)
				}
			})
		}
	}
}

func TestBadInput(t *testing.T) {
	d := t.TempDir()
	for _, o := range []Options{
		{Name: "Bad Name", Arch: "layered", Router: "stdlib", Dir: d},
		{Name: "ok", Arch: "nope", Router: "stdlib", Dir: d},
		{Name: "ok", Arch: "layered", Router: "nope", Dir: d},
		{Name: "ok", Arch: "layered", Router: "stdlib", DB: "nope", Dir: d},
		{Name: "ok", Arch: "layered", Router: "stdlib", Extras: []string{"nope"}, Dir: d},
	} {
		if Generate(o) == nil {
			t.Errorf("expected error for %+v", o)
		}
	}
}

func TestAddFeatureErrors(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "lay", Arch: "layered", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/lay"
	if err := AddFeature(p, "billing"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"billing", "greeting", "Bad", "type", ""} {
		if AddFeature(p, name) == nil {
			t.Errorf("AddFeature(%q) should fail", name)
		}
	}
	if AddModule(p, "x") == nil {
		t.Error("add module on layered should fail")
	}
}

func TestCheckCatchesViolations(t *testing.T) {
	for arch, bad := range map[string][2]string{
		"layered":   {"repository", "service"},
		"hexagonal": {"domain", "store"},
		"modular":   {"modules/greeting", "handler"},
	} {
		d := t.TempDir()
		if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: arch, Router: "stdlib", Dir: d}); err != nil {
			t.Fatal(err)
		}
		p := d + "/demo"
		src := "package x\nimport _ \"example.com/demo/internal/" + bad[1] + "\"\n"
		if err := os.WriteFile(p+"/internal/"+bad[0]+"/bad.go", []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := check.Run(p)
		if err != nil || len(got) != 1 || !strings.Contains(got[0], "must not import") {
			t.Errorf("%s: got %v, %v", arch, got, err)
		}
	}
}

// One full-stack project per architecture: chi + pgx + every extra.
func TestFullStackBuilds(t *testing.T) {
	archs, _ := fs.ReadDir(templates.FS, "arch")
	extras, _ := fs.ReadDir(templates.FS, "extras")
	var xs []string
	for _, e := range extras {
		xs = append(xs, e.Name())
	}
	for _, a := range archs {
		t.Run(a.Name(), func(t *testing.T) {
			dir := t.TempDir()
			if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: a.Name(), Router: "chi", DB: "pgx", Extras: xs, Dir: dir}); err != nil {
				t.Fatal(err)
			}
			p := dir + "/demo"
			for _, f := range []string{".github/workflows/ci.yml", "Dockerfile", ".dockerignore", "compose.yaml", "internal/telemetry/telemetry.go", "internal/database/database.go"} {
				if _, err := os.Stat(p + "/" + f); err != nil {
					t.Error(err)
				}
			}
			goRun(t, p, "build", "./...")
			goRun(t, p, "vet", "./...")
			if out, _ := exec.Command("gofmt", "-l", p).Output(); len(out) > 0 {
				t.Errorf("not gofmt-clean:\n%s", out)
			}
			if bad, err := check.Run(p); err != nil || len(bad) > 0 {
				t.Errorf("check: %v %v", bad, err)
			}
		})
	}
}

func TestInteractiveNew(t *testing.T) {
	d := t.TempDir()
	in := strings.NewReader("demo\n\nmodular\n\n\nci\n") // name, default module, arch, default router, no db, ci
	var out strings.Builder
	if err := Run([]string{"new", "--dir", d}, in, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(d + "/demo/kybernes.yaml")
	for _, want := range []string{"module: demo", "architecture: modular", "router: stdlib", "extras: [ci]"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("manifest missing %q:\n%s", want, b)
		}
	}
	if _, err := os.Stat(d + "/demo/.github/workflows/ci.yml"); err != nil {
		t.Error(err)
	}
}

package cli

import (
	"os"
	"strings"
	"testing"
)

func TestDoctor(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/demo"
	lines, failed := Doctor(p)
	if failed {
		t.Fatalf("fresh project should be healthy:\n%s", strings.Join(lines, "\n"))
	}

	// stale manifest version and a leftover merge file -> warnings, still not failed
	b, _ := os.ReadFile(p + "/kybernes.yaml")
	os.WriteFile(p+"/kybernes.yaml", []byte(strings.Replace(string(b), "kybernes: "+version, "kybernes: v0.0.1", 1)), 0o644)
	os.WriteFile(p+"/internal/handler/handler.go.kybernes-new", nil, 0o644)
	lines, failed = Doctor(p)
	got := strings.Join(lines, "\n")
	if failed || !strings.Contains(got, "run `kybernes update`") || !strings.Contains(got, "handler.go.kybernes-new") {
		t.Errorf("expected two warnings, failed=%v:\n%s", failed, got)
	}

	// module mismatch and an architecture violation -> failures
	g, _ := os.ReadFile(p + "/go.mod")
	os.WriteFile(p+"/go.mod", []byte(strings.Replace(string(g), "example.com/demo", "example.com/other", 1)), 0o644)
	os.WriteFile(p+"/internal/repository/bad.go", []byte("package repository\nimport _ \"example.com/demo/internal/service\"\n"), 0o644)
	lines, failed = Doctor(p)
	got = strings.Join(lines, "\n")
	if !failed || !strings.Contains(got, "does not match") || !strings.Contains(got, "must not import") {
		t.Errorf("expected failures:\n%s", got)
	}

	if _, failed := Doctor(t.TempDir()); !failed {
		t.Error("empty dir is not a Kybernes project")
	}
}

func TestCmpVer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.24", "1.24.0", 0}, {"1.24.1", "1.24", 1}, {"1.22", "1.27.3", -1}, {"1.9", "1.10", -1}} {
		if got := cmpVer(c.a, c.b); got != c.want {
			t.Errorf("cmpVer(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

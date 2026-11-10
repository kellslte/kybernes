package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/kybernes/kybernes/internal/lock"
)

func TestUpdate(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/demo"
	if err := AddFeature(p, "billing"); err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Read(p)["internal/service/billing.go"]; !ok {
		t.Fatal("add feature did not record files in the lock")
	}
	if r, err := Update(p, false); err != nil || len(r) != 0 {
		t.Fatalf("fresh project should be up to date: %v %v", r, err)
	}

	const (
		x = "internal/service/greeting.go"    // unmodified locally, template "changed"
		y = "internal/handler/handler.go"     // edited locally AND template changed
		z = "internal/repository/greeting.go" // edited locally, template unchanged
		w = "README.md"                       // deleted locally
		n = ".gitignore"                      // new in the template
	)
	read := func(f string) string { b, _ := os.ReadFile(p + "/" + f); return string(b) }
	write := func(f, s string) {
		if err := os.WriteFile(p+"/"+f, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lk := lock.Read(p)
	orig := read(x)
	write(x, "old template output")
	lk[x] = lock.Hash([]byte("old template output"))
	write(y, "my edits")
	lk[y] = lock.Hash([]byte("old template output"))
	write(z, "my edits")
	os.Remove(p + "/" + w)
	os.Remove(p + "/" + n)
	delete(lk, n)
	if err := lock.Write(p, lk); err != nil {
		t.Fatal(err)
	}

	// dry run reports but touches nothing
	if _, err := Update(p, true); err != nil || read(x) != "old template output" {
		t.Fatalf("dry run changed files: %v", err)
	}

	r, err := Update(p, false)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r, "\n")
	for _, want := range []string{"updated   " + x, "conflict  " + y, "kept      " + z, "skipped   " + w, "created   " + n} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
	if read(x) != orig {
		t.Error("unmodified file was not updated")
	}
	if read(y) != "my edits" || read(y+".kybernes-new") == "" {
		t.Error("conflict must keep local file and write .kybernes-new")
	}
	if read(z) != "my edits" {
		t.Error("kept file was overwritten")
	}
	if _, err := os.Stat(p + "/" + w); err == nil {
		t.Error("locally deleted file was recreated")
	}
	if _, err := os.Stat(p + "/" + n); err != nil {
		t.Error("new template file not created")
	}
}

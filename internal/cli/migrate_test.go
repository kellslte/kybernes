package cli

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kellslte/kybernes/internal/check"
	"github.com/kellslte/kybernes/templates"
)

func TestAddMigration(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", DB: "pgx", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/demo"
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	rel, err := AddMigration(p, "create_users", now)
	if err != nil || rel != "migrations/20261110120000_create_users.sql" {
		t.Fatalf("got %q, %v", rel, err)
	}
	b, _ := os.ReadFile(p + "/" + rel)
	if !strings.Contains(string(b), "-- +goose Up") || !strings.Contains(string(b), "-- +goose Down") {
		t.Errorf("missing goose markers:\n%s", b)
	}
	if _, err := AddMigration(p, "create_users", now); err == nil {
		t.Error("duplicate should be refused")
	}
	if _, err := AddMigration(p, "Bad-Name", now); err == nil {
		t.Error("invalid name should be refused")
	}
	goRun(t, p, "build", "./...") // embeds the new file too

	if err := Generate(Options{Name: "nodb", Arch: "layered", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddMigration(d+"/nodb", "x", now); err == nil {
		t.Error("add migration without a database layer should fail")
	}
	if Migrate(d+"/nodb", []string{"up"}, io.Discard) == nil {
		t.Error("migrate without a database layer should fail")
	}
}

// Every database layer: generates, builds, vets, is gofmt-clean and passes check.
func TestDatabaseLayers(t *testing.T) {
	dbs, _ := fs.ReadDir(templates.FS, "db")
	for _, db := range dbs {
		t.Run(db.Name(), func(t *testing.T) {
			d := t.TempDir()
			if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", DB: db.Name(), Dir: d}); err != nil {
				t.Fatal(err)
			}
			p := d + "/demo"
			for _, f := range []string{"internal/database/database.go", "compose.yaml"} {
				if db.Name() == "sqlite" && f == "compose.yaml" {
					continue // SQLite needs no server
				}
				if _, err := os.Stat(p + "/" + f); err != nil {
					t.Error(err)
				}
			}
			_, err := os.Stat(p + "/cmd/migrate/main.go")
			if (err == nil) != hasMigrations(db.Name()) {
				t.Errorf("cmd/migrate presence (%v) disagrees with hasMigrations", err == nil)
			}
			goRun(t, p, "build", "./...")
			goRun(t, p, "vet", "./...")
			if out, _ := exec.Command("gofmt", "-l", p).Output(); len(out) > 0 {
				t.Errorf("not gofmt-clean:\n%s", out)
			}
			if bad, err := check.Run(p); err != nil || len(bad) > 0 {
				t.Errorf("check: %v %v", bad, err)
			}
			if lines, failed := Doctor(p); failed {
				t.Errorf("doctor:\n%s", strings.Join(lines, "\n"))
			}
			if !hasMigrations(db.Name()) {
				if _, err := AddMigration(p, "x", time.Now()); err == nil || !strings.Contains(err.Error(), "no SQL migrations") {
					t.Errorf("add migration should be refused for %s, got %v", db.Name(), err)
				}
			}
		})
	}
}

// SQLite needs no server, so this runs real goose migrations end to end.
func TestSQLiteMigratesForReal(t *testing.T) {
	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", DB: "sqlite", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/demo"
	t.Setenv("DATABASE_URL", "file:"+p+"/test.db")
	rel, err := AddMigration(p, "create_notes", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sqlText := "-- +goose Up\nCREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);\n\n-- +goose Down\nDROP TABLE notes;\n"
	if err := os.WriteFile(p+"/"+rel, []byte(sqlText), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Migrate(p, []string{"up"}, &out); err != nil {
		t.Fatalf("up: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "create_notes") {
		t.Errorf("up output should mention the migration:\n%s", out.String())
	}
	out.Reset()
	if err := Migrate(p, []string{"status"}, &out); err != nil || strings.Contains(out.String(), "Pending") {
		t.Errorf("status after up: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := Migrate(p, []string{"down"}, &out); err != nil {
		t.Fatalf("down: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(p + "/test.db"); err != nil {
		t.Error("expected the sqlite database file to exist")
	}
}

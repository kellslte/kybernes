package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/kellslte/kybernes/internal/manifest"
	"github.com/kellslte/kybernes/templates"
)

var migRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// hasMigrations reports whether a database layer ships goose migrations (SQL databases do, MongoDB does not).
func hasMigrations(db string) bool {
	_, err := fs.Stat(templates.FS, "db/"+db+"/migrations")
	return err == nil
}

func needMigrations(m map[string]string) error {
	switch db := m["database"]; {
	case db == "":
		return fmt.Errorf("project has no database layer (create it with --db %s)", options("db"))
	case !hasMigrations(db):
		return fmt.Errorf("the %s layer has no SQL migrations", db)
	}
	return nil
}

// AddMigration creates migrations/<UTC timestamp>_<name>.sql with goose Up/Down markers.
func AddMigration(dir, name string, now time.Time) (string, error) {
	m, err := manifest.Read(dir)
	if err != nil {
		return "", err
	}
	if err := needMigrations(m); err != nil {
		return "", err
	}
	if !migRe.MatchString(name) {
		return "", fmt.Errorf("invalid migration name %q: lowercase letters, digits, underscores", name)
	}
	rel := fmt.Sprintf("migrations/%s_%s.sql", now.UTC().Format("20060102150405"), name)
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if _, err := os.Stat(p); err == nil {
		return "", fmt.Errorf("%s already exists", rel)
	}
	body := "-- +goose Up\n\n\n-- +goose Down\n\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// Migrate runs `go run ./cmd/migrate <args>` in the project (goose with the embedded migrations).
func Migrate(dir string, args []string, out io.Writer) error {
	m, err := manifest.Read(dir)
	if err != nil {
		return err
	}
	if err := needMigrations(m); err != nil {
		return err
	}
	cmd := exec.Command("go", append([]string{"run", "./cmd/migrate"}, args...)...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out // goose logs to stderr
	return cmd.Run()
}

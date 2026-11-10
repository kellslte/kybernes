package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/kellslte/kybernes/internal/check"
	"github.com/kellslte/kybernes/internal/lock"
	"github.com/kellslte/kybernes/internal/render"
	"github.com/kellslte/kybernes/templates"
)

const version = "v0.1.0"

const usage = `usage:
  kybernes new [name] [--module path] [--arch layered|hexagonal|modular] [--router stdlib|chi] [--db pgx] [--extras ci,docker,otel] [--dir .]
  kybernes add feature <name> [--dir .]
  kybernes add module <name> [--dir .]            (alias of add feature; modular only)
  kybernes add handler <name> [--dir .]           (HTTP handler in internal/handler)
  kybernes add service <name> [--module m] [--dir .]   (service in the architecture's business layer; --module required for modular)
  kybernes add migration <name> [--dir .]         (goose SQL file in migrations/; needs --db)
  kybernes migrate [--dir .] <up|down|status|...> (run goose migrations via DATABASE_URL; needs --db)
  kybernes dev [--dir .]                          (build, run and live-reload cmd/server on file changes)
  kybernes update [--dry-run] [--dir .]           (merge newer templates; local edits are never overwritten)
  kybernes check [--dir .]                        (verify import rules for the project's architecture)
  kybernes doctor [--dir .]                       (diagnose toolchain and project health)`

func Run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "new":
		return runNew(args[1:], in, out)
	case "add":
		return runAdd(args[1:], out)
	case "update":
		return runUpdate(args[1:], out)
	case "check":
		return runCheck(args[1:], out)
	case "doctor":
		return runDoctor(args[1:], out)
	case "dev":
		fs := flag.NewFlagSet("dev", flag.ContinueOnError)
		dir := fs.String("dir", ".", "project directory")
		if _, err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return Dev(ctx, *dir, out)
	case "migrate":
		args = args[1:]
		dir := "."
		if len(args) >= 2 && args[0] == "--dir" {
			dir, args = args[1], args[2:]
		}
		if len(args) == 0 {
			return errors.New(usage)
		}
		return Migrate(dir, args, out)
	}
	return errors.New(usage)
}

// parseFlags parses fs over args, allowing one positional name before or after the flags.
func parseFlags(fs *flag.FlagSet, args []string) (string, error) {
	fs.SetOutput(io.Discard)
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("%w\n%s", err, usage)
	}
	if name == "" {
		name = fs.Arg(0)
	}
	return name, nil
}

func runNew(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	var o Options
	fs.StringVar(&o.Module, "module", "", "go module path (default: name)")
	fs.StringVar(&o.Arch, "arch", "layered", "architecture")
	fs.StringVar(&o.Router, "router", "stdlib", "router")
	fs.StringVar(&o.DB, "db", "", "database layer (e.g. pgx)")
	extras := fs.String("extras", "", "comma-separated extras (ci,docker,otel)")
	fs.StringVar(&o.Dir, "dir", ".", "parent directory")
	name, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	o.Name = name
	if *extras != "" {
		o.Extras = strings.Split(*extras, ",")
	}
	if o.Name == "" { // no name given: ask for everything
		r := bufio.NewReader(in)
		o.Name = ask(r, out, "Project name", "", "")
		o.Module = ask(r, out, "Go module path", o.Name, "")
		o.Arch = ask(r, out, "Architecture", o.Arch, options("arch"))
		o.Router = ask(r, out, "Router", o.Router, options("router"))
		if o.DB = ask(r, out, "Database", "none", options("db")); o.DB == "none" {
			o.DB = ""
		}
		if x := ask(r, out, "Extras (comma-separated, or none)", "none", options("extras")); x != "none" {
			o.Extras = strings.Split(strings.ReplaceAll(x, " ", ""), ",")
		}
	}
	if err := Generate(o); err != nil {
		return err
	}
	fmt.Fprintf(out, "created %s (%s, %s)\n  cd %s && go run ./cmd/server\n", o.Name, o.Arch, o.Router, o.Name)
	return nil
}

func runAdd(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	mod := fs.String("module", "", "target module (add service, modular only)")
	name, err := parseFlags(fs, args[1:])
	if err != nil {
		return err
	}
	var files []string
	switch args[0] {
	case "feature":
		err = AddFeature(*dir, name)
	case "module":
		err = AddModule(*dir, name)
	case "handler":
		files, err = AddHandler(*dir, name)
	case "service":
		files, err = AddService(*dir, name, *mod)
	case "migration":
		var f string
		if f, err = AddMigration(*dir, name, time.Now()); err == nil {
			files = []string{f}
		}
	default:
		return errors.New(usage)
	}
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(out, "added %s %s: wire it up in cmd/server/main.go and internal/handler\n", args[0], name)
		return nil
	}
	for _, f := range files {
		fmt.Fprintln(out, "created", f)
	}
	return nil
}

func runUpdate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	dry := fs.Bool("dry-run", false, "report without writing")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	report, err := Update(*dir, *dry)
	if err != nil {
		return err
	}
	for _, l := range report {
		fmt.Fprintln(out, l)
	}
	if len(report) == 0 {
		fmt.Fprintln(out, "already up to date")
	}
	return nil
}

func runCheck(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	bad, err := check.Run(*dir)
	if err != nil {
		return err
	}
	for _, v := range bad {
		fmt.Fprintln(out, v)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d architecture violation(s)", len(bad))
	}
	fmt.Fprintln(out, "ok: no architecture violations")
	return nil
}

func runDoctor(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	lines, failed := Doctor(*dir)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	if failed {
		return errors.New("doctor found problems")
	}
	return nil
}

type Options struct {
	Name, Module, Arch, Router, DB, Dir string
	Extras                              []string
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Generate overlays base, arch/<Arch>, router/<Router>, db and extras into <Dir>/<Name>.
func Generate(o Options) error {
	if !nameRe.MatchString(o.Name) {
		return fmt.Errorf("invalid name %q: use lowercase letters, digits, hyphens", o.Name)
	}
	if o.Module == "" {
		o.Module = o.Name
	}
	dst := filepath.Join(o.Dir, o.Name)
	if es, err := os.ReadDir(dst); err == nil && len(es) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dst)
	}
	paths, err := renderTo(dst, o, nil)
	if err != nil {
		return err
	}
	if err := tidy(dst); err != nil {
		return err
	}
	return lock.Record(dst, paths)
}

// renderTo writes the whole project (layers, then the given features) into dst.
func renderTo(dst string, o Options, features []string) ([]string, error) {
	layers := []string{"base"}
	pick := func(kind, val string) error {
		if _, err := fs.Stat(templates.FS, kind+"/"+val); err != nil {
			return fmt.Errorf("unknown %s %q (available: %s)", kind, val, options(kind))
		}
		layers = append(layers, kind+"/"+val)
		return nil
	}
	if err := pick("arch", o.Arch); err != nil {
		return nil, err
	}
	if err := pick("router", o.Router); err != nil {
		return nil, err
	}
	if o.DB != "" {
		if err := pick("db", o.DB); err != nil {
			return nil, err
		}
	}
	for _, x := range o.Extras {
		if err := pick("extras", x); err != nil {
			return nil, err
		}
	}
	data := map[string]string{"Name": o.Name, "Module": o.Module, "Arch": o.Arch, "Router": o.Router, "DB": o.DB, "Version": version, "Extras": "", "Otel": ""}
	if len(o.Extras) > 0 {
		data["Extras"] = "[" + strings.Join(o.Extras, ", ") + "]"
	}
	if slices.Contains(o.Extras, "otel") {
		data["Otel"] = "true"
	}
	var all []string
	for _, l := range layers {
		sub, _ := fs.Sub(templates.FS, l)
		w, err := render.Apply(dst, sub, data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l, err)
		}
		all = append(all, w...)
	}
	for _, f := range features {
		w, err := renderFeature(dst, o.Arch, o.Module, f)
		if err != nil {
			return nil, err
		}
		all = append(all, w...)
	}
	return all, nil
}

// tidy resolves third-party imports added by router/db/extras layers (no-op for stdlib-only).
func tidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, b)
	}
	return nil
}

func options(kind string) string {
	es, _ := fs.ReadDir(templates.FS, kind)
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return strings.Join(n, ", ")
}

// ask prints a prompt and returns the trimmed answer, or def when the line is empty.
func ask(r *bufio.Reader, out io.Writer, label, def, opts string) string {
	if opts != "" {
		label += " (" + opts + ")"
	}
	if def != "" {
		label += " [" + def + "]"
	}
	fmt.Fprint(out, label+": ")
	line, _ := r.ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return def
}

package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kybernes/kybernes/internal/check"
	"github.com/kybernes/kybernes/internal/lock"
	"github.com/kybernes/kybernes/internal/render"
	"github.com/kybernes/kybernes/templates"
)

const version = "v0.1.0"

const usage = `usage:
  kybernes new <name> [--module path] [--arch layered|hexagonal|modular] [--router stdlib|chi] [--db pgx] [--extras ci,docker,otel] [--dir .]
  kybernes add feature <name> [--dir .]
  kybernes update [--dry-run] [--dir .]   (merge newer templates; local edits are never overwritten)
  kybernes check [--dir .]   (verify import rules for the project's architecture)
  kybernes add module <name> [--dir .]   (alias of add feature; modular architecture only)`

func Run(args []string, in io.Reader, out io.Writer) error {
	if len(args) >= 3 && args[0] == "add" && (args[1] == "feature" || args[1] == "module") {
		dir := "."
		if len(args) == 5 && args[3] == "--dir" {
			dir = args[4]
		} else if len(args) != 3 {
			return errors.New(usage)
		}
		add := AddFeature
		if args[1] == "module" {
			add = AddModule
		}
		if err := add(dir, args[2]); err != nil {
			return err
		}
		fmt.Fprintf(out, "added %s %s: wire it up in cmd/server/main.go and internal/handler\n", args[1], args[2])
		return nil
	}
	if len(args) >= 1 && args[0] == "update" {
		dir, dry := ".", false
		for i := 1; i < len(args); i++ {
			switch {
			case args[i] == "--dry-run":
				dry = true
			case args[i] == "--dir" && i+1 < len(args):
				i++
				dir = args[i]
			default:
				return errors.New(usage)
			}
		}
		report, err := Update(dir, dry)
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
	if len(args) >= 1 && args[0] == "check" {
		dir := "."
		if len(args) == 3 && args[1] == "--dir" {
			dir = args[2]
		} else if len(args) != 1 {
			return errors.New(usage)
		}
		bad, err := check.Run(dir)
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
	if len(args) == 0 || args[0] != "new" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o Options
	fs.StringVar(&o.Module, "module", "", "go module path (default: name)")
	fs.StringVar(&o.Arch, "arch", "layered", "architecture")
	fs.StringVar(&o.Router, "router", "stdlib", "router")
	fs.StringVar(&o.DB, "db", "", "database layer (e.g. pgx)")
	extras := fs.String("extras", "", "comma-separated extras (ci,docker,otel)")
	fs.StringVar(&o.Dir, "dir", ".", "parent directory")
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") { // name may precede flags
		o.Name, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	if o.Name == "" {
		o.Name = fs.Arg(0)
	}
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

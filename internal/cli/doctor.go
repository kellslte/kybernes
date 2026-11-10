package cli

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kellslte/kybernes/internal/check"
	"github.com/kellslte/kybernes/internal/lock"
	"github.com/kellslte/kybernes/internal/manifest"
)

var (
	goVerRe = regexp.MustCompile(`go(\d+\.\d+(?:\.\d+)?)`)
	modRe2  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	goDirRe = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)`)
)

// cmpVer compares dotted versions numerically ("1.24" == "1.24.0").
func cmpVer(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Doctor diagnoses the toolchain and the project at dir. Lines start with ok/warn/fail.
func Doctor(dir string) (lines []string, failed bool) {
	add := func(level, format string, a ...any) {
		lines = append(lines, fmt.Sprintf("%-5s %s", level, fmt.Sprintf(format, a...)))
		failed = failed || level == "fail"
	}

	have := ""
	if b, err := exec.Command("go", "version").Output(); err != nil {
		add("fail", "go toolchain not found on PATH")
	} else if m := goVerRe.FindStringSubmatch(string(b)); m != nil {
		have = m[1]
		add("ok", "go toolchain %s", have)
	}

	m, err := manifest.Read(dir)
	if err != nil {
		add("fail", "not a Kybernes project: %v", err)
		return
	}
	add("ok", "kybernes.yaml: %s architecture, %s router", m["architecture"], m["router"])

	if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err != nil {
		add("fail", "go.mod: %v", err)
	} else {
		if mm := modRe2.FindSubmatch(b); mm == nil || string(mm[1]) != m["module"] {
			add("fail", "go.mod module does not match kybernes.yaml (%s)", m["module"])
		}
		if gm := goDirRe.FindSubmatch(b); gm != nil && have != "" && cmpVer(string(gm[1]), have) > 0 {
			add("fail", "go.mod needs Go %s, installed is %s", gm[1], have)
		} else {
			add("ok", "go.mod consistent with manifest and toolchain")
		}
	}

	if m["kybernes"] != version {
		add("warn", "generated with kybernes %s, this is %s: run `kybernes update`", m["kybernes"], version)
	}
	if len(lock.Read(dir)) == 0 {
		add("warn", "kybernes.lock missing or empty: `update` cannot tell your edits from generated code")
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if err == nil && strings.HasSuffix(p, ".kybernes-new") {
			rel, _ := filepath.Rel(dir, p)
			add("warn", "unmerged update left %s: merge it, then delete it", rel)
		}
		return nil
	})

	if bad, err := check.Run(dir); err != nil {
		add("fail", "architecture check: %v", err)
	} else if len(bad) > 0 {
		for _, v := range bad {
			add("fail", "%s", v)
		}
	} else {
		add("ok", "architecture rules respected")
	}

	if m["database"] != "" && hasMigrations(m["database"]) {
		if es, _ := filepath.Glob(filepath.Join(dir, "migrations", "*.sql")); len(es) == 0 {
			add("fail", "database layer enabled but migrations/ has no .sql files")
		} else {
			add("ok", "%d migration(s)", len(es))
		}
	}
	if strings.Contains(m["extras"], "docker") {
		if _, err := exec.LookPath("docker"); err != nil {
			add("warn", "docker extra enabled but docker is not on PATH")
		}
	}
	return
}

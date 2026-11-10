package cli

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Dev builds and runs ./cmd/server, rebuilding and restarting it whenever a .go, .sql,
// go.mod or go.sum file changes. A failed build keeps the previous server running.
// It returns when ctx is cancelled.
//
// ponytail: polls file mtimes every 500ms instead of using fsnotify (no dependency,
// fine for project-sized trees). Swap in fsnotify if polling shows up in profiles.
func Dev(ctx context.Context, dir string, out io.Writer) error {
	return devLoop(ctx, dir, out, 500*time.Millisecond)
}

func devLoop(ctx context.Context, dir string, out io.Writer, poll time.Duration) error {
	if st, err := os.Stat(filepath.Join(dir, "cmd", "server")); err != nil || !st.IsDir() {
		return fmt.Errorf("%s has no cmd/server to run", dir)
	}
	bin := filepath.Join(dir, "bin", "server")
	next := filepath.Join(dir, "bin", ".server.next")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	logf := func(format string, a ...any) { fmt.Fprintf(out, "[kybernes] "+format+"\n", a...) }

	var cur *exec.Cmd
	var exited chan struct{}
	stop := func() {
		if cur == nil {
			return
		}
		select {
		case <-exited: // already gone
		default:
			_ = cur.Process.Signal(os.Interrupt) // generated servers shut down gracefully
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				_ = cur.Process.Kill()
				<-exited
			}
		}
		cur = nil
	}
	defer stop()

	restart := func() {
		logf("building...")
		build := exec.CommandContext(ctx, "go", "build", "-o", next, "./cmd/server")
		build.Dir, build.Stdout, build.Stderr = dir, out, out
		if err := build.Run(); err != nil {
			if ctx.Err() == nil {
				logf("build failed, keeping the previous server (if any): %v", err)
			}
			return
		}
		stop()
		if err := os.Rename(next, bin); err != nil {
			logf("%v", err)
			return
		}
		c := exec.Command(bin)
		c.Dir, c.Stdout, c.Stderr = dir, out, out
		if err := c.Start(); err != nil {
			logf("start failed: %v", err)
			return
		}
		cur, exited = c, make(chan struct{})
		go func(c *exec.Cmd, done chan struct{}) {
			err := c.Wait()
			if ctx.Err() == nil && err != nil {
				logf("server exited: %v (waiting for changes)", err)
			}
			close(done)
		}(c, exited)
		logf("running")
	}

	last := fingerprint(dir)
	restart()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
		fp := fingerprint(dir)
		if fp == last {
			continue
		}
		for { // debounce: wait until edits settle
			time.Sleep(poll / 2)
			if again := fingerprint(dir); again != fp {
				fp = again
				continue
			}
			break
		}
		last = fp
		logf("change detected")
		restart()
	}
}

// fingerprint hashes the path, mtime and size of every watched file under dir.
func fingerprint(dir string) uint64 {
	h := fnv.New64a()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".sql") || n == "go.mod" || n == "go.sum" {
			if info, err := d.Info(); err == nil {
				fmt.Fprintf(h, "%s|%d|%d\n", p, info.ModTime().UnixNano(), info.Size())
			}
		}
		return nil
	})
	return h.Sum64()
}

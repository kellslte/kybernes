package cli

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func body(url string) string {
	c := http.Client{Timeout: time.Second}
	r, err := c.Get(url)
	if err != nil {
		return ""
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return strings.TrimSpace(string(b))
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDevReloads(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	t.Setenv("ADDR", addr)

	d := t.TempDir()
	if err := Generate(Options{Name: "demo", Module: "example.com/demo", Arch: "layered", Router: "stdlib", Dir: d}); err != nil {
		t.Fatal(err)
	}
	p := d + "/demo"
	url := "http://" + addr + "/hello"

	out := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- devLoop(ctx, p, out, 100*time.Millisecond) }()
	defer cancel()

	waitFor(t, "first start", func() bool { return body(url) == "Hello, world!" })

	repo := p + "/internal/repository/greeting.go"
	src, _ := os.ReadFile(repo)
	if err := os.WriteFile(repo, []byte(strings.Replace(string(src), `"Hello"`, `"Howdy"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reload after edit", func() bool { return body(url) == "Howdy, world!" })

	// a broken edit must not take the running server down
	if err := os.WriteFile(repo, []byte("package repository\nthis is not go"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "build failure message", func() bool { return strings.Contains(out.String(), "build failed") })
	if got := body(url); got != "Howdy, world!" {
		t.Errorf("previous server should keep running after a failed build, got %q", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Dev did not return after cancel")
	}
	waitFor(t, "server stopped", func() bool { return body(url) == "" })
}

func TestDevNeedsServer(t *testing.T) {
	if err := Dev(context.Background(), t.TempDir(), io.Discard); err == nil {
		t.Error("dir without cmd/server should fail")
	}
}

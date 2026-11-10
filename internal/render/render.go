// Package render copies a template tree to disk; files ending in .tmpl are
// executed with text/template and the suffix is dropped.
package render

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Apply renders src into dst and returns the files written (slash paths relative to dst). "__mod__" in a path is replaced by data["Mod"].
func Apply(dst string, src fs.FS, data map[string]string) ([]string, error) {
	var written []string
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out := filepath.Join(dst, filepath.FromSlash(strings.ReplaceAll(p, "__mod__", data["Mod"])))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".tmpl") {
			t, err := template.New(p).Option("missingkey=error").Parse(string(b))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return err
			}
			b, out = buf.Bytes(), strings.TrimSuffix(out, ".tmpl")
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, out)
		written = append(written, filepath.ToSlash(rel))
		return nil
	})
	return written, err
}

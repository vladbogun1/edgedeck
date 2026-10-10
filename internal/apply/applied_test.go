package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladbogun1/edgedeck/internal/routes"
)

const routesA = `version: 1
routes:
    - domain: a.example.com
      path: /
      type: stub
      security_headers: false
      hsts: false
`

const routesB = routesA + `    - domain: b.example.com
      path: /
      type: stub
      security_headers: false
      hsts: false
`

func setup(t *testing.T) *Applier {
	t.Helper()
	edge := t.TempDir()
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(edge, "sites"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routes.Path(edge), []byte(routesA), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := routes.Load(edge)
	for name, body := range routes.Render(cfg.Routes) {
		if err := os.WriteFile(filepath.Join(edge, "sites", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &Applier{EdgeDir: edge, SnapshotDir: filepath.Join(data, "snapshots"), AppliedDir: filepath.Join(data, "applied")}
	if err := a.EnsureApplied(); err != nil {
		t.Fatal(err)
	}
	return a
}

// Сценарий s3 10.10.2026: routes.yaml поправили руками, применение откатилось.
func TestRollbackRestoresRoutesYAML(t *testing.T) {
	a := setup(t)
	if d, _ := a.CheckDrift(); !d.Empty() {
		t.Fatalf("сразу после заведения расхождений быть не должно: %+v", d)
	}

	// Правка руками.
	if err := os.WriteFile(routes.Path(a.EdgeDir), []byte(routesB), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := a.CheckDrift()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 1 || d.Added[0] != "b.example.com/" {
		t.Fatalf("ждали неприменённый b.example.com/, получили %+v", d)
	}

	// Снимок перед применением должен содержать применённое (A), а не правку (B).
	snap, err := a.snapshot("test", "проверка")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(a.SnapshotDir, snap, "routes.yaml"))
	if string(b) != routesA {
		t.Fatalf("в снимке не применённый routes.yaml:\n%s", b)
	}

	// Откат: routes.yaml возвращается к A, правка B сохранена.
	kept, err := a.restoreKeep(snap)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(routes.Path(a.EdgeDir))
	if string(now) != routesA {
		t.Fatalf("после отката routes.yaml не вернулся:\n%s", now)
	}
	saved, err := os.ReadFile(kept)
	if err != nil || !strings.Contains(string(saved), "b.example.com") {
		t.Fatalf("отвергнутая правка не сохранена (%q): %v", kept, err)
	}
	if d, _ := a.CheckDrift(); !d.Empty() {
		t.Fatalf("после отката расхождений быть не должно: %+v", d)
	}
}

func TestDiscardAndHandEditedFile(t *testing.T) {
	a := setup(t)
	if err := os.WriteFile(routes.Path(a.EdgeDir), []byte(routesB), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Discard(); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(routes.Path(a.EdgeDir))
	if string(now) != routesA {
		t.Fatal("Discard не вернул routes.yaml")
	}

	// Сгенерированный файл поправили руками — это тоже расхождение.
	f := filepath.Join(a.EdgeDir, "sites", "a.example.com.caddy")
	body, _ := os.ReadFile(f)
	_ = os.WriteFile(f, append(body, []byte("# руками\n")...), 0o644)
	d, _ := a.CheckDrift()
	if len(d.Files) != 1 || d.Files[0] != "a.example.com.caddy" {
		t.Fatalf("ручная правка файла не замечена: %+v", d)
	}
}

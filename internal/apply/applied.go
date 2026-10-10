package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/vladbogun1/edgedeck/internal/routes"
)

// «Последнее применённое» состояние.
//
// routes.yaml можно править руками и применять из командной строки. Раньше снимок
// делался уже после такой правки, и автооткат возвращал файлы сайтов, а
// routes.yaml оставался с изменениями: файл описывал то, чего на сервере нет, и
// следующее применение по любому поводу молча выкатывало (или снимало) чужие
// маршруты. Так было на s3 10.10.2026.
//
// Теперь панель хранит копию routes.yaml, который последним прошёл контроль, в
// data/applied/. Снимок берёт routes.yaml оттуда, откат возвращает именно его,
// а отвергнутая правка не теряется — кладётся в data/rejected/.

func (a *Applier) appliedPath() string { return filepath.Join(a.AppliedDir, "routes.yaml") }

func (a *Applier) rejectedDir() string { return filepath.Join(filepath.Dir(a.AppliedDir), "rejected") }

// EnsureApplied заводит копию при первом запуске: считаем, что то, что сейчас в
// routes.yaml, и работает (так было на всех серверах до появления этой копии).
func (a *Applier) EnsureApplied() error {
	if a.AppliedDir == "" {
		return nil
	}
	if _, err := os.Stat(a.appliedPath()); err == nil {
		return nil
	}
	return a.markApplied()
}

// markApplied запоминает текущий routes.yaml как применённый.
func (a *Applier) markApplied() error {
	if a.AppliedDir == "" {
		return nil
	}
	b, err := os.ReadFile(routes.Path(a.EdgeDir))
	if os.IsNotExist(err) {
		b = nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(a.AppliedDir, 0o700); err != nil {
		return err
	}
	tmp := a.appliedPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.appliedPath())
}

// appliedRoutesFile — содержимое применённого routes.yaml для снимка. Если копии
// ещё нет — то, что на диске.
func (a *Applier) appliedRoutesFile() ([]byte, error) {
	if a.AppliedDir != "" {
		if b, err := os.ReadFile(a.appliedPath()); err == nil {
			return b, nil
		}
	}
	return os.ReadFile(routes.Path(a.EdgeDir))
}

// keepRejected сохраняет routes.yaml с диска, если он отличается от того, что
// сейчас вернётся из снимка. Возвращает путь сохранённой копии или "".
func (a *Applier) keepRejected(restoring []byte) string {
	cur, err := os.ReadFile(routes.Path(a.EdgeDir))
	if err != nil || string(cur) == string(restoring) || a.AppliedDir == "" {
		return ""
	}
	if err := os.MkdirAll(a.rejectedDir(), 0o700); err != nil {
		return ""
	}
	p := filepath.Join(a.rejectedDir(), "routes-"+time.Now().UTC().Format("20060102-150405")+".yaml")
	if os.WriteFile(p, cur, 0o600) != nil {
		return ""
	}
	return p
}

// Discard возвращает routes.yaml к применённому состоянию; правка сохраняется в
// data/rejected/. Файлы сайтов не трогает — они и так соответствуют применённому.
func (a *Applier) Discard() (string, error) {
	b, err := os.ReadFile(a.appliedPath())
	if err != nil {
		return "", fmt.Errorf("нет копии применённого routes.yaml: %w", err)
	}
	kept := a.keepRejected(b)
	if err := routes.WriteFileAsDirOwner(routes.Path(a.EdgeDir), b); err != nil {
		return "", err
	}
	return kept, nil
}

// Drift — чем диск отличается от применённого.
type Drift struct {
	Added   []string `json:"added"`   // маршруты есть в routes.yaml, но не применены
	Removed []string `json:"removed"` // применены, но из routes.yaml убраны
	Changed []string `json:"changed"` // изменены, но не применены
	// Files — сгенерированные файлы сайтов, которые отличаются от того, что выдал
	// бы генератор из применённого routes.yaml (кто-то правил их руками).
	Files []string `json:"files"`
}

func (d Drift) Empty() bool {
	return len(d.Added)+len(d.Removed)+len(d.Changed)+len(d.Files) == 0
}

// Summary — одна строка для CLI и уведомлений.
func (d Drift) Summary() string {
	var parts []string
	for _, x := range d.Added {
		parts = append(parts, "+"+x)
	}
	for _, x := range d.Removed {
		parts = append(parts, "−"+x)
	}
	for _, x := range d.Changed {
		parts = append(parts, "~"+x)
	}
	return strings.Join(parts, ", ")
}

// CheckDrift сравнивает routes.yaml на диске с применённым и сгенерированные
// файлы — с тем, что из применённого получилось бы.
func (a *Applier) CheckDrift() (Drift, error) {
	var d Drift
	if a.AppliedDir == "" {
		return d, nil
	}
	applied, err := loadConfigFile(a.appliedPath())
	if err != nil {
		return d, err
	}
	onDisk, err := routes.Load(a.EdgeDir)
	if err != nil {
		return d, err
	}
	was := map[string]routes.Route{}
	for _, r := range applied.Routes {
		was[r.ID()] = r
	}
	now := map[string]routes.Route{}
	for _, r := range onDisk.Routes {
		now[r.ID()] = r
		old, ok := was[r.ID()]
		switch {
		case !ok:
			d.Added = append(d.Added, r.ID())
		case !reflect.DeepEqual(old, r):
			d.Changed = append(d.Changed, r.ID())
		}
	}
	for id := range was {
		if _, ok := now[id]; !ok {
			d.Removed = append(d.Removed, id)
		}
	}

	wanted := routes.Render(applied.Routes)
	current, manual, err := a.readSites()
	if err != nil {
		return d, err
	}
	isManual := map[string]bool{}
	for _, m := range manual {
		isManual[m] = true
	}
	for name, body := range current {
		if isManual[name] {
			continue
		}
		if want, ok := wanted[name]; !ok || want != body {
			d.Files = append(d.Files, name)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Changed)
	sort.Strings(d.Files)
	return d, nil
}

// loadConfigFile читает routes.yaml по произвольному пути тем же разбором.
func loadConfigFile(path string) (routes.Config, error) {
	dir, err := os.MkdirTemp("", "edgedeck-applied-")
	if err != nil {
		return routes.Config{}, err
	}
	defer os.RemoveAll(dir)
	b, err := os.ReadFile(path)
	if err != nil {
		return routes.Config{}, err
	}
	if err := os.WriteFile(routes.Path(dir), b, 0o600); err != nil {
		return routes.Config{}, err
	}
	return routes.Load(dir)
}

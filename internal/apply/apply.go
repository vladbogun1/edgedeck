// Package apply — применение изменений как транзакция.
//
// Порядок жёсткий и объясняется опытом (DESIGN.md §5.2):
//
//  1. проверка формы маршрутов        (без сети)
//  2. сборка конфига в памяти
//  3. валидация в Caddy через /adapt  — до записи на диск: битый конфиг
//     не должен даже приближаться к прод
//  4. эталон: как отвечают ВСЕ домены прямо сейчас
//  5. снимок текущих файлов
//  6. запись файлов
//  7. применение через /load          — без разрыва соединений
//  8. контроль: домены отвечают не хуже, чем в эталоне
//  9. если стало хуже — автоматический откат к снимку
//
// Пункт 8 существует потому, что ломается обычно не то, что менял.
package apply

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vladbogun1/edgedeck/internal/routes"
)

// Caddy — то, что нам нужно от admin API (реализует internal/caddyx).
type Caddy interface {
	Validate(ctx context.Context, caddyfile []byte) error
	Load(ctx context.Context, caddyfile []byte) error
}

type Applier struct {
	EdgeDir     string
	SnapshotDir string
	Caddy       Caddy
	// CaddyHost — имя контейнера прокси в сети edge: через него идёт контроль,
	// то есть запросы к домену ровно так, как их сделает браузер.
	CaddyHost string
	// AppliedDir — где лежит копия последнего применённого routes.yaml (applied.go).
	AppliedDir string
}

// Options — параметры применения.
type Options struct {
	Actor  string
	Reason string
	// Adopt — файлы, написанные руками, которые пользователь осознанно решил
	// отдать под управление панели (кнопка «принять под управление»).
	// Без этого списка панель чужие файлы не перезаписывает никогда.
	Adopt []string
}

func (o Options) adoptSet() map[string]bool {
	out := map[string]bool{}
	for _, f := range o.Adopt {
		out[f] = true
	}
	return out
}

// Change — одна строка плана.
type Change struct {
	Kind   string `json:"kind"` // create | update | delete | unchanged
	File   string `json:"file"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Plan — что произойдёт, если применить.
type Plan struct {
	Changes  []Change `json:"changes"`
	Warnings []string `json:"warnings"`
	Valid    bool     `json:"valid"`
	Error    string   `json:"error,omitempty"`
}

// Result — что произошло.
type Result struct {
	Applied    bool              `json:"applied"`
	RolledBack bool              `json:"rolled_back"`
	Snapshot   string            `json:"snapshot,omitempty"`
	Before     map[string]string `json:"before"`
	After      map[string]string `json:"after"`
	Message    string            `json:"message"`
	Changes    []Change          `json:"changes"`
	// Rejected — куда сохранён отвергнутый откатом routes.yaml (если был другим).
	Rejected string `json:"rejected,omitempty"`
}

// ---- план и валидация ----

func (a *Applier) Plan(ctx context.Context, cfg routes.Config, adopt map[string]bool) Plan {
	p := Plan{}
	wanted := routes.Render(cfg.Routes)

	current, manual, err := a.sitesState(adopt)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	for m := range manual {
		if _, clash := wanted[m]; clash {
			p.Warnings = append(p.Warnings,
				"файл "+m+" написан руками — панель его не тронет; удалите его или снимите маршрут, иначе домен будет описан дважды")
		}
	}

	names := map[string]bool{}
	for f := range wanted {
		names[f] = true
	}
	for f := range current {
		names[f] = true
	}
	var files []string
	for f := range names {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, f := range files {
		before, hadBefore := current[f]
		after, wantAfter := wanted[f]
		// Файл, написанный руками, панель не пишет и не удаляет. В плане он
		// должен выглядеть неизменным, иначе план обещает то, чего не сделает.
		if manual[f] {
			p.Changes = append(p.Changes, Change{Kind: "unchanged", File: f})
			continue
		}
		switch {
		case wantAfter && !hadBefore:
			p.Changes = append(p.Changes, Change{Kind: "create", File: f, After: after})
		case wantAfter && hadBefore && before != after:
			p.Changes = append(p.Changes, Change{Kind: "update", File: f, Before: before, After: after})
		case !wantAfter && hadBefore:
			p.Changes = append(p.Changes, Change{Kind: "delete", File: f, Before: before})
		default:
			p.Changes = append(p.Changes, Change{Kind: "unchanged", File: f})
		}
	}

	caddyfile, err := a.assemble(wanted, adopt)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	if err := a.Caddy.Validate(ctx, caddyfile); err != nil {
		p.Error = err.Error()
		return p
	}
	p.Valid = true
	return p
}

// ---- применение ----

func (a *Applier) Apply(ctx context.Context, cfg routes.Config, opts Options) (Result, error) {
	res := Result{}
	adopt := opts.adoptSet()

	plan := a.Plan(ctx, cfg, adopt)
	res.Changes = plan.Changes
	if !plan.Valid {
		return res, fmt.Errorf("конфиг не прошёл проверку, ничего не менял: %s", plan.Error)
	}

	// Эталон: как всё отвечает до изменения.
	domains := a.knownDomains(cfg)
	res.Before = a.probeAll(ctx, domains)

	snapshot, err := a.snapshot(opts.Actor, opts.Reason)
	if err != nil {
		return res, fmt.Errorf("не смог сделать снимок, поэтому и менять не стал: %w", err)
	}
	res.Snapshot = snapshot

	wanted := routes.Render(cfg.Routes)
	if err := a.writeSites(wanted, adopt); err != nil {
		_ = a.restore(snapshot)
		return res, fmt.Errorf("запись файлов не удалась, вернул как было: %w", err)
	}
	if err := routes.Save(a.EdgeDir, cfg); err != nil {
		_ = a.restore(snapshot)
		return res, fmt.Errorf("не смог записать routes.yaml, вернул как было: %w", err)
	}

	caddyfile, err := a.assembleFromDisk()
	if err != nil {
		_ = a.restore(snapshot)
		return res, err
	}
	if err := a.Caddy.Load(ctx, caddyfile); err != nil {
		_ = a.restore(snapshot)
		_ = a.reloadFromDisk(ctx)
		return res, fmt.Errorf("применить не удалось, вернул как было: %w", err)
	}
	res.Applied = true

	// Контроль. Сначала быстро, потом ещё раз: сертификат нового домена
	// выпускается несколько секунд, и первая проверка может застать его на полпути.
	res.After = a.probeAll(ctx, domains)
	if broken := regressions(res.Before, res.After); len(broken) > 0 {
		time.Sleep(6 * time.Second)
		res.After = a.probeAll(ctx, domains)
		broken = regressions(res.Before, res.After)
		if len(broken) > 0 {
			if kept, err := a.restoreKeep(snapshot); err == nil {
				_ = a.reloadFromDisk(ctx)
				res.RolledBack = true
				res.Rejected = kept
				res.Message = "стало хуже (" + strings.Join(broken, "; ") + ") — откатил к состоянию до изменения, routes.yaml тоже"
				if kept != "" {
					res.Message += "; ваша правка сохранена в " + kept
				}
				return res, nil
			}
			res.Message = "стало хуже (" + strings.Join(broken, "; ") + "), но откат не удался — смотрите снимок " + snapshot
			return res, nil
		}
	}
	if err := a.markApplied(); err != nil {
		res.Message = "применено, остальные домены отвечают как раньше; но копию применённого routes.yaml записать не смог: " + err.Error()
		return res, nil
	}
	res.Message = "применено, остальные домены отвечают как раньше"
	return res, nil
}

// ProbeDomains опрашивает все известные домены — нужно сторожу, который следит
// за сайтами между изменениями.
func (a *Applier) ProbeDomains(ctx context.Context, cfg routes.Config) map[string]string {
	return a.probeAll(ctx, a.knownDomains(cfg))
}

// regressions — что стало хуже. Улучшения и одинаковые ответы игнорируем:
// панель не должна ругаться на то, что сайт, который и так лежал, лежит дальше.
func regressions(before, after map[string]string) []string {
	var out []string
	for domain, was := range before {
		now := after[domain]
		if was == now {
			continue
		}
		if isOK(was) && !isOK(now) {
			out = append(out, domain+": было "+was+", стало "+now)
		}
	}
	sort.Strings(out)
	return out
}

func isOK(state string) bool {
	if state == "" {
		return false
	}
	// «200», «301», «404» — сайт отвечает. Всё, что начинается с «ошибка» или 5xx — нет.
	if strings.HasPrefix(state, "ошибка") {
		return false
	}
	return !strings.HasPrefix(state, "5")
}

// ---- проверка доменов через сам прокси ----

// probeAll спрашивает каждый домен у Caddy так же, как это сделает браузер:
// TLS с нужным SNI, потом обычный GET. Сертификат не проверяем — нас интересует
// маршрутизация, а не доверие (для заходов по IP он вообще самоподписанный).
func (a *Applier) probeAll(ctx context.Context, domains []string) map[string]string {
	out := map[string]string{}
	type res struct {
		domain string
		state  string
	}
	ch := make(chan res, len(domains))
	for _, d := range domains {
		go func(d string) {
			ch <- res{d, a.probe(ctx, d)}
		}(d)
	}
	for range domains {
		r := <-ch
		out[r.domain] = r.state
	}
	return out
}

func (a *Applier) probe(ctx context.Context, domain string) string {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialTLSContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, a.CaddyHost+":443")
				if err != nil {
					return nil, err
				}
				tlsConn := tls.Client(conn, &tls.Config{
					ServerName:         domain,
					InsecureSkipVerify: true,
				})
				if err := tlsConn.HandshakeContext(ctx); err != nil {
					_ = conn.Close()
					return nil, err
				}
				return tlsConn, nil
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return "ошибка: " + err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return "ошибка: " + shortErr(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	return fmt.Sprintf("%d", resp.StatusCode)
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// knownDomains — все домены, которые обслуживает сервер: и из маршрутов, и из
// файлов, написанных руками. Контроль обязан смотреть на чужие сайты тоже.
func (a *Applier) knownDomains(cfg routes.Config) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	for _, r := range cfg.Routes {
		add(r.Domain)
		for _, al := range r.Aliases {
			add(al)
		}
	}
	current, manual, err := a.readSites()
	if err == nil {
		for _, f := range manual {
			for _, d := range domainsInFile(current[f]) {
				add(d)
			}
		}
	}
	sort.Strings(out)
	return out
}

var siteAddrRe = regexp.MustCompile(`(?m)^([a-z0-9][^\s{,]*\.[a-z]{2,}[^\s{,]*)(,\s*[^\s{,]+)*\s*\{`)

func domainsInFile(body string) []string {
	var out []string
	for _, m := range siteAddrRe.FindAllStringSubmatch(body, -1) {
		line := strings.TrimSuffix(strings.TrimSpace(m[0]), "{")
		for _, part := range strings.Split(line, ",") {
			d := strings.TrimSpace(part)
			d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
			if d != "" && strings.Contains(d, ".") {
				out = append(out, strings.ToLower(d))
			}
		}
	}
	return out
}

// ---- файлы сайтов ----

func (a *Applier) sitesDir() string { return filepath.Join(a.EdgeDir, "sites") }

// sitesState — содержимое файлов и множество «написанных руками» с учётом того,
// что пользователь мог согласиться отдать конкретные файлы под управление.
func (a *Applier) sitesState(adopt map[string]bool) (map[string]string, map[string]bool, error) {
	content, manual, err := a.readSites()
	if err != nil {
		return nil, nil, err
	}
	set := map[string]bool{}
	for _, m := range manual {
		if !adopt[m] {
			set[m] = true
		}
	}
	return content, set, nil
}

// readSites возвращает содержимое всех файлов сайтов и список тех, что написаны
// руками (без нашей шапки) — такие панель не трогает.
func (a *Applier) readSites() (map[string]string, []string, error) {
	entries, err := os.ReadDir(a.sitesDir())
	if err != nil {
		return nil, nil, fmt.Errorf("каталог сайтов %s: %w", a.sitesDir(), err)
	}
	content := map[string]string{}
	var manual []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".caddy") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.sitesDir(), e.Name()))
		if err != nil {
			continue
		}
		body := string(b)
		content[e.Name()] = body
		if !strings.Contains(body, routes.GeneratedMarker) {
			manual = append(manual, e.Name())
		}
	}
	return content, manual, nil
}

func (a *Applier) writeSites(wanted map[string]string, adopt map[string]bool) error {
	current, manualSet, err := a.sitesState(adopt)
	if err != nil {
		return err
	}

	for name, body := range wanted {
		if manualSet[name] {
			// Файл руками: не перетираем молча — это правило §5.3.
			continue
		}
		if current[name] == body {
			continue
		}
		if err := routes.WriteFileAsDirOwner(filepath.Join(a.sitesDir(), name), []byte(body)); err != nil {
			return err
		}
	}
	// Удаляем только свои файлы, которых больше нет в маршрутах.
	for name := range current {
		if _, keep := wanted[name]; keep || manualSet[name] {
			continue
		}
		if err := os.Remove(filepath.Join(a.sitesDir(), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ---- сборка Caddyfile для валидации и применения ----

var importRe = regexp.MustCompile(`(?m)^\s*import\s+/etc/caddy/([a-zA-Z0-9_-]+)/\*\.caddy\s*$`)

// assemble подставляет содержимое файлов вместо строк import.
//
// Так конфиг можно проверить целиком, ничего не записав на диск: admin API
// принимает только самодостаточный текст, а пути внутри контейнера от него не
// видны. Заодно это единственный способ сделать «проверку до записи».
func (a *Applier) assemble(sites map[string]string, adopt map[string]bool) ([]byte, error) {
	base, err := os.ReadFile(filepath.Join(a.EdgeDir, "Caddyfile"))
	if err != nil {
		return nil, fmt.Errorf("Caddyfile: %w", err)
	}
	current, manualSet, err := a.sitesState(adopt)
	if err != nil {
		return nil, err
	}

	// Итоговый набор файлов: наши сгенерированные + чужие, написанные руками.
	final := map[string]string{}
	for name, body := range current {
		if manualSet[name] {
			final[name] = body
		}
	}
	for name, body := range sites {
		if !manualSet[name] {
			final[name] = body
		}
	}

	out := importRe.ReplaceAllStringFunc(string(base), func(line string) string {
		dir := importRe.FindStringSubmatch(line)[1]
		var b strings.Builder
		fmt.Fprintf(&b, "# ↓ вместо import /etc/caddy/%s/*.caddy\n", dir)
		if dir == "sites" {
			names := make([]string, 0, len(final))
			for n := range final {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				b.WriteString(final[n])
				b.WriteString("\n")
			}
			return b.String()
		}
		files, _ := filepath.Glob(filepath.Join(a.EdgeDir, dir, "*.caddy"))
		sort.Strings(files)
		for _, f := range files {
			if body, err := os.ReadFile(f); err == nil {
				b.Write(body)
				b.WriteString("\n")
			}
		}
		return b.String()
	})
	return []byte(out), nil
}

func (a *Applier) assembleFromDisk() ([]byte, error) {
	current, _, err := a.readSites()
	if err != nil {
		return nil, err
	}
	return a.assemble(current, nil)
}

func (a *Applier) reloadFromDisk(ctx context.Context) error {
	cfg, err := a.assembleFromDisk()
	if err != nil {
		return err
	}
	return a.Caddy.Load(ctx, cfg)
}

// ReloadFromDisk применяет то, что сейчас лежит в файлах. Нужно после отката и
// после ручных правок.
func (a *Applier) ReloadFromDisk(ctx context.Context) error { return a.reloadFromDisk(ctx) }

// ---- снимки ----

type SnapshotMeta struct {
	Name    string    `json:"name"`
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`
	Reason  string    `json:"reason"`
	Files   int       `json:"files"`
	Current bool      `json:"current"`
}

// snapshot копирует routes.yaml и все файлы сайтов в каталог со временем в имени.
// Обычные файлы, а не git: их можно посмотреть и вернуть руками по SSH, даже
// если панель мертва (DESIGN.md §2.19).
func (a *Applier) snapshot(actor, reason string) (string, error) {
	name := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(a.SnapshotDir, name)
	if err := os.MkdirAll(filepath.Join(dir, "sites"), 0o700); err != nil {
		return "", err
	}
	files := 0
	current, _, err := a.readSites()
	if err != nil {
		return "", err
	}
	for fname, body := range current {
		if err := os.WriteFile(filepath.Join(dir, "sites", fname), []byte(body), 0o600); err != nil {
			return "", err
		}
		files++
	}
	// routes.yaml — из применённого состояния, а не с диска: на диске может уже
	// лежать правка, которую сейчас и применяют (applied.go).
	if b, err := a.appliedRoutesFile(); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "routes.yaml"), b, 0o600); err != nil {
			return "", err
		}
	}
	meta := SnapshotMeta{Name: name, Time: time.Now(), Actor: actor, Reason: reason, Files: files}
	if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o600)
	}
	a.trimSnapshots(50)
	return name, nil
}

func (a *Applier) Snapshots() []SnapshotMeta {
	entries, err := os.ReadDir(a.SnapshotDir)
	if err != nil {
		return nil
	}
	var out []SnapshotMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta := SnapshotMeta{Name: e.Name()}
		if b, err := os.ReadFile(filepath.Join(a.SnapshotDir, e.Name(), "meta.json")); err == nil {
			_ = json.Unmarshal(b, &meta)
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

func (a *Applier) trimSnapshots(keep int) {
	list := a.Snapshots()
	for i, s := range list {
		if i >= keep {
			_ = os.RemoveAll(filepath.Join(a.SnapshotDir, s.Name))
		}
	}
}

// Restore возвращает файлы из снимка и применяет их.
func (a *Applier) Restore(ctx context.Context, name string) error {
	if err := a.restore(name); err != nil {
		return err
	}
	if err := a.reloadFromDisk(ctx); err != nil {
		return err
	}
	// Вернули состояние руками — теперь оно и есть применённое.
	return a.markApplied()
}

func (a *Applier) restore(name string) error {
	_, err := a.restoreKeep(name)
	return err
}

// restoreKeep возвращает файлы и routes.yaml из снимка. Если routes.yaml на диске
// отличался от снимка, его копия кладётся в data/rejected/ — путь возвращается.
func (a *Applier) restoreKeep(name string) (string, error) {
	dir := filepath.Join(a.SnapshotDir, filepath.Base(name))
	entries, err := os.ReadDir(filepath.Join(dir, "sites"))
	if err != nil {
		return "", fmt.Errorf("снимок %s не читается: %w", name, err)
	}
	// Сначала убираем свои файлы, которых в снимке нет, потом восстанавливаем.
	inSnapshot := map[string]bool{}
	for _, e := range entries {
		inSnapshot[e.Name()] = true
	}
	current, manualSet, err := a.sitesState(nil)
	if err != nil {
		return "", err
	}
	for fname := range current {
		if inSnapshot[fname] || manualSet[fname] {
			continue
		}
		_ = os.Remove(filepath.Join(a.sitesDir(), fname))
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, "sites", e.Name()))
		if err != nil {
			return "", err
		}
		if err := routes.WriteFileAsDirOwner(filepath.Join(a.sitesDir(), e.Name()), b); err != nil {
			return "", err
		}
	}
	kept := ""
	if b, err := os.ReadFile(filepath.Join(dir, "routes.yaml")); err == nil {
		kept = a.keepRejected(b)
		if err := routes.WriteFileAsDirOwner(routes.Path(a.EdgeDir), b); err != nil {
			return "", err
		}
	}
	return kept, nil
}

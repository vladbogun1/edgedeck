// Package sites — чтение существующих конфигов Caddy.
//
// На этапе 0 панель ничего не пишет: она показывает то, что уже настроено
// руками через bin/newsite. Разбор нарочно терпимый — конфиг мог быть написан
// человеком как угодно, и лучше показать домен без деталей, чем упасть.
//
// На этапе 1 источником истины станет routes.yaml, а эти файлы будут из него
// генерироваться (DESIGN.md §5.1); разбор останется нужен для импорта.
package sites

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Site — один файл sites/*.caddy.
type Site struct {
	File      string   `json:"file"`
	Addresses []string `json:"addresses"` // домены или :80 / https://
	Kind      string   `json:"kind"`      // proxy | static | redirect | mixed | unknown
	Targets   []string `json:"targets"`   // куда проксирует
	Paths     []string `json:"paths"`     // отдельные обрабатываемые пути
	Managed   bool     `json:"managed"`   // сгенерировано панелью
	IsDefault bool     `json:"is_default"`
	SizeBytes int      `json:"size_bytes"`
}

var (
	// Начало блока сайта: адреса до «{» в начале строки (не сниппет «(name) {»).
	blockRe   = regexp.MustCompile(`(?m)^([^\s#{}(][^{}\n]*?)\s*\{\s*$`)
	proxyRe   = regexp.MustCompile(`(?m)^\s*reverse_proxy\s+([^\s{]+)`)
	rootRe    = regexp.MustCompile(`(?m)^\s*root\s+\*\s+(\S+)`)
	redirRe   = regexp.MustCompile(`(?m)^\s*redir\s+(\S+)`)
	handleRe  = regexp.MustCompile(`(?m)^\s*(?:handle|handle_path|route)\s+(/\S*)`)
	managedRe = regexp.MustCompile(`(?i)сгенерировано\s+edgedeck|generated\s+by\s+edgedeck`)
)

// Load читает каталог sites/ целиком.
func Load(dir string) ([]Site, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Site
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".caddy") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, parse(e.Name(), string(b)))
	}
	sort.Slice(out, func(i, j int) bool {
		// Дефолтный блок всегда последним: на главном экране он не сайт, а служебное.
		if out[i].IsDefault != out[j].IsDefault {
			return out[j].IsDefault
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

func parse(name, body string) Site {
	s := Site{
		File:      name,
		Managed:   managedRe.MatchString(body),
		IsDefault: strings.HasPrefix(name, "_default"),
		SizeBytes: len(body),
		Kind:      "unknown",
	}

	for _, m := range blockRe.FindAllStringSubmatch(stripComments(body), -1) {
		for _, addr := range strings.Split(m[1], ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" || strings.HasPrefix(addr, "@") {
				continue
			}
			s.Addresses = append(s.Addresses, addr)
		}
	}
	for _, m := range proxyRe.FindAllStringSubmatch(body, -1) {
		s.Targets = append(s.Targets, m[1])
	}
	for _, m := range rootRe.FindAllStringSubmatch(body, -1) {
		s.Targets = append(s.Targets, "статика: "+m[1])
	}
	for _, m := range redirRe.FindAllStringSubmatch(body, -1) {
		s.Targets = append(s.Targets, "редирект: "+m[1])
	}
	for _, m := range handleRe.FindAllStringSubmatch(body, -1) {
		s.Paths = append(s.Paths, m[1])
	}

	switch {
	case proxyRe.MatchString(body) && rootRe.MatchString(body):
		s.Kind = "mixed"
	case proxyRe.MatchString(body):
		s.Kind = "proxy"
	case rootRe.MatchString(body):
		s.Kind = "static"
	case redirRe.MatchString(body):
		s.Kind = "redirect"
	}
	s.Addresses = uniq(s.Addresses)
	s.Targets = uniq(s.Targets)
	s.Paths = uniq(s.Paths)
	return s
}

// Domains — только настоящие домены из всех сайтов (без :80, https:// и IP-блоков).
// Нужны, чтобы понимать, какой Host уже настроен, а какой пришёл впервые.
func Domains(list []Site) []string {
	var out []string
	for _, s := range list {
		if s.IsDefault {
			continue
		}
		for _, a := range s.Addresses {
			d := normalizeAddress(a)
			if d != "" {
				out = append(out, d)
			}
		}
	}
	return uniq(out)
}

// normalizeAddress вырезает схему, порт и путь: «https://example.com:443» → «example.com».
func normalizeAddress(a string) string {
	a = strings.TrimSpace(a)
	a = strings.TrimPrefix(strings.TrimPrefix(a, "https://"), "http://")
	if i := strings.IndexAny(a, "/"); i >= 0 {
		a = a[:i]
	}
	if i := strings.LastIndex(a, ":"); i >= 0 {
		// «:80» — это адрес без домена, а «example.com:8443» — домен с портом.
		if i == 0 {
			return ""
		}
		a = a[:i]
	}
	if a == "" || a == "*" || !strings.Contains(a, ".") {
		return ""
	}
	return strings.ToLower(a)
}

func stripComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if idx := strings.Index(l, "#"); idx >= 0 {
			lines[i] = l[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

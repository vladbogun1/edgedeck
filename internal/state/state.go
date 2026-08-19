// Package state — то немногое, что панель помнит сама.
//
// Никакой базы: журнал — JSONL, замеченные домены — один JSON-файл. Причина в
// DESIGN.md §2.19: всё состояние должно читаться глазами по SSH, если панель
// умерла. И заодно не тащим CGO-зависимости на arm64.
package state

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// SeenDomain — домен, который пришёл на сервер.
//
// Панель узнаёт о таких сама, потому что она и есть обработчик по умолчанию:
// любой Host, для которого нет маршрута, физически попадает в неё (DESIGN.md §9.0).
// Логи Caddy для этого разбирать не нужно.
type SeenDomain struct {
	Host       string    `json:"host"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Hits       int       `json:"hits"`
	TLSAsked   bool      `json:"tls_asked"` // Caddy спрашивал разрешение на сертификат
	TLSAllowed bool      `json:"tls_allowed"`
}

type Store struct {
	seenFile  string
	auditFile string

	mu    sync.Mutex
	seen  map[string]*SeenDomain
	dirty bool
}

func NewStore(seenFile, auditFile string) *Store {
	s := &Store{seenFile: seenFile, auditFile: auditFile, seen: map[string]*SeenDomain{}}
	if b, err := os.ReadFile(seenFile); err == nil {
		var list []*SeenDomain
		if json.Unmarshal(b, &list) == nil {
			for _, d := range list {
				if d != nil && d.Host != "" {
					s.seen[d.Host] = d
				}
			}
		}
	}
	return s
}

// NoteHost отмечает, что на панель пришёл запрос с таким Host.
// Порт и регистр отбрасываем: «Example.COM:443» и «example.com» — одно и то же.
func (s *Store) NoteHost(host string, tlsAsked, tlsAllowed bool) {
	host = normalizeHost(host)
	if host == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.seen[host]
	if !ok {
		d = &SeenDomain{Host: host, FirstSeen: now}
		s.seen[host] = d
	}
	d.LastSeen = now
	d.Hits++
	if tlsAsked {
		d.TLSAsked = true
		d.TLSAllowed = tlsAllowed
	}
	s.dirty = true
}

// Seen отдаёт замеченные домены, кроме тех, что уже настроены.
func (s *Store) Seen(configured []string) []SeenDomain {
	skip := map[string]bool{}
	for _, c := range configured {
		skip[normalizeHost(c)] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SeenDomain, 0, len(s.seen))
	for host, d := range s.seen {
		if skip[host] {
			continue
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

func (s *Store) Forget(host string) {
	host = normalizeHost(host)
	s.mu.Lock()
	delete(s.seen, host)
	s.dirty = true
	s.mu.Unlock()
}

// Flush сохраняет замеченные домены, если что-то менялось. Вызывается по таймеру:
// писать файл на каждый запрос к панели незачем.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	list := make([]*SeenDomain, 0, len(s.seen))
	for _, d := range s.seen {
		list = append(list, d)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	tmp := s.seenFile + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, s.seenFile) == nil {
		s.dirty = false
	}
}

// Event — строка журнала действий.
type Event struct {
	Time    time.Time      `json:"time"`
	Kind    string         `json:"kind"` // login, login_failed, apply, connect_network, ...
	Actor   string         `json:"actor"`
	Message string         `json:"message"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// Audit дописывает событие в журнал. Ошибку записи глотаем сознательно:
// невозможность вести журнал не должна ломать работу панели, но в логе она видна.
func (s *Store) Audit(kind, actor, message string, extra map[string]any) {
	ev := Event{Time: time.Now(), Kind: kind, Actor: actor, Message: message, Extra: extra}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.auditFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// LastEvents читает хвост журнала для экрана «История».
func (s *Store) LastEvents(limit int) []Event {
	b, err := os.ReadFile(s.auditFile)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]Event, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- { // новые сверху
		var ev Event
		if json.Unmarshal([]byte(lines[i]), &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

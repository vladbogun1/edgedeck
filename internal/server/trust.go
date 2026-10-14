package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/vladbogun1/edgedeck/internal/routes"
	"github.com/vladbogun1/edgedeck/internal/sites"
)

// Свои и чужие зоны.
//
// Панель — обработчик по умолчанию, и раньше она отвечала любому домену, чья
// A-запись смотрит на сервер: показывала форму входа и витрину сайтов, а Caddy
// выпускал на такое имя настоящий сертификат. На практике (октябрь 2026, сервер
// 132.145.132.80) это выглядело так: чей-то фишинговый *.amzcomverif.kozow.com
// с wildcard-записью на наш IP получил три десятка сертификатов Let's Encrypt,
// а сканеры антифишинга десятками тысяч запросов ходили смотреть, что там.
// Отсюда прямая дорога в блок-листы — уже нашего адреса.
//
// Поэтому домен «свой», только если его зона (имя, купленное у регистратора:
// eTLD+1 по Public Suffix List) уже встречается среди настроенных сайтов или её
// явно одобрили в панели. Удобство от этого не страдает: новый поддомен своей
// зоны по-прежнему сразу открывает панель с сертификатом, а совсем новую зону
// одобряют одной кнопкой в списке «пришли, но не настроены».
//
// Чужим доменам панель не отвечает ничем (421 без тела), сертификат им не
// выпускается. В списке они остаются — чтобы было видно, кто к нам ходит.

const trustCacheTTL = 15 * time.Second

type trustStore struct {
	file    string
	edgeDir string
	extra   []string // EDGEDECK_TRUSTED_ZONES

	mu         sync.Mutex
	approved   map[string]bool
	configured map[string]bool
	loadedAt   time.Time
}

func newTrustStore(dataDir, edgeDir string) *trustStore {
	t := &trustStore{
		file:     filepath.Join(dataDir, "trusted-zones.json"),
		edgeDir:  edgeDir,
		approved: map[string]bool{},
	}
	for _, z := range strings.Split(os.Getenv("EDGEDECK_TRUSTED_ZONES"), ",") {
		if z = zoneOf(z); z != "" {
			t.extra = append(t.extra, z)
		}
	}
	if b, err := os.ReadFile(t.file); err == nil {
		var list []string
		if json.Unmarshal(b, &list) == nil {
			for _, z := range list {
				if z = zoneOf(z); z != "" {
					t.approved[z] = true
				}
			}
		}
	}
	return t
}

// wildcardDNS — публичные сервисы, где любое имя с адресом внутри резолвится в
// этот адрес: pivonia.150-136-40-217.sslip.io → 150.136.40.217. Такое имя никто
// не покупал и не заводил, поэтому «зона» у него — само имя: своё только то, что
// стоит в маршрутах или одобрено руками. Иначе соседние www.www.pivonia… тоже
// «свои», и сканер CT-логов раскручивает петлю сертификатов (октябрь 2026, s3).
var wildcardDNS = []string{
	"sslip.io", "nip.io", "xip.io", "nip.direct", "traefik.me",
	"localtest.me", "lvh.me", "vcap.me", "lacolhost.com",
}

// isWildcardDNS — имя из такого сервиса (но не сам сервис).
func isWildcardDNS(host string) bool {
	for _, s := range wildcardDNS {
		if strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// zoneOf — зона домена: «www.mail.foo.kozow.com» → «foo.kozow.com» (kozow.com
// в Public Suffix List как публичный DDNS), «app.demo.chisetup.com.ua» →
// «chisetup.com.ua». Для IP и внутренних имён — пусто.
func zoneOf(host string) string {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(host, "*.")
	if host == "" || net.ParseIP(strings.Trim(host, "[]")) != nil || !strings.Contains(host, ".") {
		return ""
	}
	if isWildcardDNS(host) {
		return host
	}
	z, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return z
}

// Trusted решает, отвечать ли панели на такой Host и выпускать ли сертификат.
// IP-адреса и внутренние имена — всегда да: так в панель заходят, пока своих
// доменов ещё нет.
func (t *trustStore) Trusted(host string) bool {
	z := zoneOf(host)
	if z == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refreshLocked()
	return t.approved[z] || t.configured[z]
}

// refreshLocked перечитывает зоны настроенных сайтов не чаще раза в 15 секунд:
// Trusted зовётся на каждый запрос к панели.
func (t *trustStore) refreshLocked() {
	if t.configured != nil && time.Since(t.loadedAt) < trustCacheTTL {
		return
	}
	set := map[string]bool{}
	for _, z := range t.extra {
		set[z] = true
	}
	if cfg, err := routes.Load(t.edgeDir); err == nil {
		for _, r := range cfg.Routes {
			set[zoneOf(r.Domain)] = true
			for _, al := range r.Aliases {
				set[zoneOf(al)] = true
			}
		}
	}
	// Сайты, которые ещё не приняты под управление, тоже свои.
	if list, err := sites.Load(filepath.Join(t.edgeDir, "sites")); err == nil {
		for _, d := range sites.Domains(list) {
			set[zoneOf(d)] = true
		}
	}
	delete(set, "")
	t.configured, t.loadedAt = set, time.Now()
}

// Approve помечает зону своей (или снимает отметку) и сохраняет список на диск.
func (t *trustStore) Approve(host string, trusted bool) (string, error) {
	z := zoneOf(host)
	if z == "" {
		return "", nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if trusted {
		t.approved[z] = true
	} else {
		delete(t.approved, z)
	}
	list := make([]string, 0, len(t.approved))
	for k := range t.approved {
		list = append(list, k)
	}
	sort.Strings(list)
	b, _ := json.MarshalIndent(list, "", "  ")
	tmp := t.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return z, err
	}
	return z, os.Rename(tmp, t.file)
}

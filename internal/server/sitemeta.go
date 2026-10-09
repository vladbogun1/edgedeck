package server

import (
	"context"
	"crypto/tls"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Проверка сайта глазами посетителя: https-запрос с нужным SNI, но не в
// интернет, а прямо в контейнер Caddy.
//
// Раньше витрина стучалась в цель reverse_proxy из сети панели, и сайты, чьи
// контейнеры живут в своих сетях (Caddy подключён к ним, панель — нет), всегда
// выглядели лежащими: на сервере 1 так «не отвечали» bogun.site, bots.bogun.site
// и chisetup.com.ua, хотя работали. Запрос через Caddy проходит тот же путь, что
// и у посетителя: маршрут, сертификат, цель — и заодно отдаёт страницу, из
// которой берутся заголовок, иконка и картинка для витрины.

type siteMeta struct {
	Online      bool
	Title       string
	Description string
	Icon        string
	Image       string
}

type metaCache struct {
	mu    sync.Mutex
	items map[string]metaEntry
}

type metaEntry struct {
	meta siteMeta
	at   time.Time
}

// Заголовок и иконка меняются редко — держим полчаса. Доступность проверяется
// заново каждый раз, когда протухает кеш витрины (минута).
const metaTTL = 30 * time.Minute

func newMetaCache() *metaCache { return &metaCache{items: map[string]metaEntry{}} }

func (c *metaCache) get(domain string) (siteMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[domain]
	if !ok || time.Since(e.at) > metaTTL {
		return siteMeta{}, false
	}
	return e.meta, true
}

func (c *metaCache) put(domain string, m siteMeta) {
	c.mu.Lock()
	c.items[domain] = metaEntry{meta: m, at: time.Now()}
	c.mu.Unlock()
}

// viaCaddy — клиент, который любое имя соединяет с Caddy на :443. Редиректы не
// проходит: «отвечает» — это любой ответ, кроме ошибки прокси, а редирект на
// чужой домен через наш Caddy всё равно не пройти.
func viaCaddy(caddyHost string) *http.Client {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(caddyHost, "443"))
			},
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 5 * time.Second,
			DisableKeepAlives:   true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// probeSite отвечает, жив ли сайт, и попутно собирает его визитку. ok=false —
// до Caddy по :443 не достучались совсем (например, сервер только на :80); тогда
// вызывающий откатывается на старую проверку цели.
func (a *app) probeSite(ctx context.Context, domain string) (m siteMeta, ok bool) {
	client := viaCaddy(a.applier.CaddyHost)
	page := "https://" + domain + "/"
	var body []byte
	for hop := 0; hop < 3; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
		if err != nil {
			return m, false
		}
		req.Header.Set("User-Agent", "EdgeDeck/витрина")
		req.Header.Set("Accept", "text/html")
		resp, err := client.Do(req)
		if err != nil {
			var opErr *net.OpError
			if hop == 0 && asOpError(err, &opErr) && opErr.Op == "dial" {
				return m, false
			}
			// TLS или таймаут — посетитель увидел бы то же самое.
			return m, true
		}
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		resp.Body.Close()
		if hop == 0 {
			m.Online = resp.StatusCode < 500 || resp.StatusCode == http.StatusNotImplemented
		}
		loc, err := resp.Location()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 || err != nil || !sameSite(loc.Hostname(), domain) {
			break
		}
		page, body = loc.String(), nil
	}
	if len(body) > 0 {
		base, _ := url.Parse(page)
		fillMeta(&m, string(body), base)
	}
	if m.Icon == "" {
		m.Icon = "https://" + domain + "/favicon.ico"
	}
	return m, true
}

// sameSite — редирект внутри того же сайта (example.com → www.example.com),
// за ним можно идти через наш Caddy.
func sameSite(host, domain string) bool {
	return zoneOf(host) != "" && zoneOf(host) == zoneOf(domain)
}

func asOpError(err error, target **net.OpError) bool {
	for err != nil {
		if e, ok := err.(*net.OpError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	tagRe   = regexp.MustCompile(`(?is)<(meta|link)\s[^>]*>`)
	attrRe  = regexp.MustCompile(`(?is)([a-z:-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// fillMeta достаёт из HTML заголовок, описание, иконку и картинку для превью.
// Полноценный парсер здесь не нужен: страница своя, а ошибка стоит одной иконки.
func fillMeta(m *siteMeta, page string, base *url.URL) {
	if t := titleRe.FindStringSubmatch(page); t != nil {
		m.Title = clean(t[1], 80)
	}
	iconRank := 0
	for _, tag := range tagRe.FindAllStringSubmatch(page, -1) {
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(tag[0], -1) {
			attrs[strings.ToLower(a[1])] = a[3] + a[4] + a[5]
		}
		if strings.EqualFold(tag[1], "meta") {
			key := strings.ToLower(attrs["property"] + attrs["name"])
			switch key {
			case "og:title":
				if t := clean(attrs["content"], 80); t != "" {
					m.Title = t
				}
			case "description", "og:description":
				if m.Description == "" || key == "og:description" {
					m.Description = clean(attrs["content"], 160)
				}
			case "og:image", "twitter:image":
				if m.Image == "" {
					m.Image = absURL(base, attrs["content"])
				}
			}
			continue
		}
		// Иконка: крупная apple-touch-icon лучше крошечной favicon.
		rank := 0
		switch rel := strings.ToLower(attrs["rel"]); {
		case strings.Contains(rel, "apple-touch-icon"):
			rank = 3
		case rel == "icon" || strings.Contains(rel, "shortcut"):
			rank = 2
			if strings.HasSuffix(strings.ToLower(attrs["href"]), ".svg") {
				rank = 4
			}
		}
		if rank > iconRank && attrs["href"] != "" {
			if u := absURL(base, attrs["href"]); u != "" {
				m.Icon, iconRank = u, rank
			}
		}
	}
}

// absURL делает ссылку абсолютной и пропускает только https и data:image —
// витрина вставляет её в <img>, ничего другого туда класть не надо.
func absURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(html.UnescapeString(ref))
	if strings.HasPrefix(ref, "data:image/") && len(ref) < 64<<10 {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil || base == nil {
		return ""
	}
	u = base.ResolveReference(u)
	if u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func clean(s string, max int) string {
	s = strings.Join(strings.Fields(html.UnescapeString(s)), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

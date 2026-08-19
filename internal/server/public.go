package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vladbogun1/edgedeck/internal/sites"
)

// Публичная витрина: список сайтов, которые здесь живут, до всякого входа.
//
// Пользователь попросил именно так: «на главной прям перед паролем — обозреватель
// уже зареганных сайтов», а управление и добавление — под паролём.
//
// Что отдаём наружу и почему это безопасно: только имена настроенных доменов и
// признак «отвечает». Домены и так публичны — они есть в DNS и в логах
// Certificate Transparency. А вот всё остальное наружу НЕ идёт:
//   - имена контейнеров и порты (это карта внутренностей сервера);
//   - домены из «пришли, но не настроены» (их ещё нет в природе, это планы и опечатки);
//   - имя сервера, адрес, версия, порты, сертификаты, журнал.
type publicSite struct {
	Domain string `json:"domain"`
	Kind   string `json:"kind"`   // сайт | статика | редирект
	Online bool   `json:"online"` // цель отвечает
}

type publicCache struct {
	mu      sync.Mutex
	sites   []publicSite
	fetched time.Time
	ttl     time.Duration
}

func newPublicCache() *publicCache {
	return &publicCache{ttl: 60 * time.Second}
}

// invalidate сбрасывает кеш витрины: после применения изменений список сайтов
// должен обновиться сразу, а не через минуту.
func (c *publicCache) invalidate() {
	c.mu.Lock()
	c.sites, c.fetched = nil, time.Time{}
	c.mu.Unlock()
}

func (a *app) handlePublicSites(w http.ResponseWriter, r *http.Request) {
	a.public.mu.Lock()
	fresh := time.Since(a.public.fetched) < a.public.ttl && a.public.sites != nil
	cached := a.public.sites
	a.public.mu.Unlock()

	if fresh {
		writeJSON(w, http.StatusOK, map[string]any{"sites": cached})
		return
	}

	list := a.buildPublicSites(r.Context())

	a.public.mu.Lock()
	a.public.sites, a.public.fetched = list, time.Now()
	a.public.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"sites": list})
}

func (a *app) buildPublicSites(ctx context.Context) []publicSite {
	siteList, err := sites.Load(a.cfg.SitesDir())
	if err != nil {
		return []publicSite{}
	}

	// Проверки цели идут параллельно: один недоступный контейнер не должен
	// задерживать всю страницу.
	type result struct {
		site publicSite
		i    int
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []result
	)

	for i, s := range siteList {
		if s.IsDefault {
			continue
		}
		domain := firstDomain(s)
		if domain == "" {
			continue
		}
		ps := publicSite{Domain: domain, Kind: kindLabel(s.Kind)}

		host, port, ok := proxyTarget(s)
		if !ok {
			// Статика и редиректы: цели-контейнера нет, проверять нечего.
			ps.Online = true
			mu.Lock()
			results = append(results, result{ps, i})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(ps publicSite, host string, port, i int) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			res := a.net.Probe(probeCtx, host, port, ps.Domain)
			ps.Online = res.TCPOpen
			mu.Lock()
			results = append(results, result{ps, i})
			mu.Unlock()
		}(ps, host, port, i)
	}
	wg.Wait()

	sort.Slice(results, func(x, y int) bool { return results[x].i < results[y].i })
	out := make([]publicSite, 0, len(results))
	for _, r := range results {
		out = append(out, r.site)
	}
	return out
}

// firstDomain — первое настоящее имя сайта. «:80» и «https://» пропускаем:
// это служебные адреса, а не домены.
func firstDomain(s sites.Site) string {
	for _, a := range s.Addresses {
		d := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(a), "https://"), "http://")
		if i := strings.IndexAny(d, "/"); i >= 0 {
			d = d[:i]
		}
		if d == "" || !strings.Contains(d, ".") {
			continue
		}
		if net := strings.Split(d, ":"); len(net) > 1 {
			d = net[0]
		}
		// Заход по IP в витрине не показываем — это не сайт.
		if isIPLike(d) {
			continue
		}
		return strings.ToLower(d)
	}
	return ""
}

func isIPLike(s string) bool {
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

// proxyTarget вытаскивает «контейнер:порт» из первой цели проксирования.
func proxyTarget(s sites.Site) (string, int, bool) {
	for _, t := range s.Targets {
		if strings.HasPrefix(t, "статика:") || strings.HasPrefix(t, "редирект:") {
			continue
		}
		t = strings.TrimPrefix(strings.TrimPrefix(t, "http://"), "https://")
		host, portStr, found := strings.Cut(t, ":")
		if !found || host == "" {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSuffix(portStr, "/"))
		if err != nil {
			continue
		}
		return host, port, true
	}
	return "", 0, false
}

func kindLabel(kind string) string {
	switch kind {
	case "static":
		return "статика"
	case "redirect":
		return "редирект"
	case "proxy", "mixed":
		return "сайт"
	default:
		return "сайт"
	}
}

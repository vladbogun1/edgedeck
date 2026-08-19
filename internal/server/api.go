package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vladbogun1/edgedeck/internal/auth"
	"github.com/vladbogun1/edgedeck/internal/caddyx"
	"github.com/vladbogun1/edgedeck/internal/hostports"
	"github.com/vladbogun1/edgedeck/internal/sites"
	"github.com/vladbogun1/edgedeck/internal/state"
	"github.com/vladbogun1/edgedeck/internal/version"
)

// ---- вход ----

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не разобрал запрос"})
		return
	}
	ip := clientIP(r)
	sess, err := a.auth.Login(req.Password, ip, r.UserAgent())
	if err != nil {
		a.state.Audit("login_failed", ip, "неудачная попытка входа", map[string]any{"host": r.Host})
		switch {
		case err == auth.ErrLocked:
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "слишком много попыток — подождите 15 минут",
			})
		default:
			// Ответ намеренно скупой: форму видно на любом ненастроенном домене.
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "неверный пароль"})
		}
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.Expires,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
	a.state.Audit("login", ip, "вход в панель", map[string]any{"host": r.Host})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.auth.Drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSession — единственное, что можно спросить без входа: «я залогинен?».
// Ничего о сервере не рассказывает.
func (a *app) handleSession(w http.ResponseWriter, r *http.Request) {
	authed := false
	if c, err := r.Cookie(sessionCookie); err == nil {
		_, authed = a.auth.Check(c.Value)
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": authed})
}

func (a *app) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не разобрал запрос"})
		return
	}
	ip := clientIP(r)
	if _, err := a.auth.Login(req.Current, ip, r.UserAgent()); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "текущий пароль не подошёл"})
		return
	}
	if err := a.auth.SetPassword(req.New); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.state.Audit("password_changed", ip, "пароль изменён", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- разрешение на сертификат ----

// askLimiter ограничивает выпуск сертификатов для новых доменов.
// Без него любой, кто направит домен на наш IP, сожжёт недельный лимит
// Let's Encrypt (DESIGN.md §2.8).
type askLimiter struct {
	limitPerHour int

	mu      sync.Mutex
	granted []time.Time
	cache   map[string]askVerdict
}

type askVerdict struct {
	allowed bool
	until   time.Time
	reason  string
}

func newAskLimiter(limit int) *askLimiter {
	return &askLimiter{limitPerHour: limit, cache: map[string]askVerdict{}}
}

func (l *askLimiter) cached(domain string) (askVerdict, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.cache[domain]
	if !ok || time.Now().After(v.until) {
		return askVerdict{}, false
	}
	return v, true
}

// remember кеширует ответ: разрешение на 5 минут, отказ на час — чтобы чужой
// домен, направленный на нас, не устраивал шторм DNS-запросов.
func (l *askLimiter) remember(domain string, allowed bool, reason string) {
	ttl := time.Hour
	if allowed {
		ttl = 5 * time.Minute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache[domain] = askVerdict{allowed: allowed, until: time.Now().Add(ttl), reason: reason}
	if allowed {
		l.granted = append(l.granted, time.Now())
	}
}

func (l *askLimiter) overLimit() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-time.Hour)
	kept := l.granted[:0]
	for _, t := range l.granted {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.granted = kept
	return len(l.granted) >= l.limitPerHour
}

func (a *app) handleTLSAsk(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("token") != askToken() {
		http.Error(w, "нет", http.StatusForbidden)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if domain == "" {
		http.Error(w, "нет домена", http.StatusBadRequest)
		return
	}

	if v, ok := a.tlsAsk.cached(domain); ok {
		a.state.NoteHost(domain, true, v.allowed)
		respondAsk(w, v.allowed)
		return
	}
	if a.tlsAsk.overLimit() {
		slog.Warn("отказ в сертификате: лимит новых доменов в час", "domain", domain)
		a.state.NoteHost(domain, true, false)
		a.state.Audit("tls_ask_denied", "caddy", "лимит новых доменов в час", map[string]any{"domain": domain})
		respondAsk(w, false)
		return
	}

	check := a.net.CheckDomain(r.Context(), domain)
	a.tlsAsk.remember(domain, check.PointsHere, check.Message)
	a.state.NoteHost(domain, true, check.PointsHere)
	if check.PointsHere {
		slog.Info("разрешил сертификат", "domain", domain)
		a.state.Audit("tls_ask_allowed", "caddy", "разрешён сертификат", map[string]any{"domain": domain})
	} else {
		slog.Warn("отказ в сертификате", "domain", domain, "почему", check.Message)
	}
	respondAsk(w, check.PointsHere)
}

func respondAsk(w http.ResponseWriter, allowed bool) {
	if allowed {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// ---- данные для интерфейса ----

type overview struct {
	Server      serverInfo           `json:"server"`
	Sites       []sites.Site         `json:"sites"`
	SeenDomains []state.SeenDomain   `json:"seen_domains"`
	Certs       []caddyx.Certificate `json:"certificates"`
	Warnings    []warning            `json:"warnings"`
	Caddy       caddyStatus          `json:"caddy"`
}

type serverInfo struct {
	Hostname    string  `json:"hostname"`
	PublicIP    string  `json:"public_ip"`
	Version     string  `json:"version"`
	LoadAvg     float64 `json:"load_avg"`
	MemTotalMB  int     `json:"mem_total_mb"`
	MemUsedMB   int     `json:"mem_used_mb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
	CPUCount    int     `json:"cpu_count"`
}

type caddyStatus struct {
	Alive   bool   `json:"alive"`
	Message string `json:"message"`
}

type warning struct {
	Level  string `json:"level"` // red | yellow
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

func (a *app) handleOverview(w http.ResponseWriter, r *http.Request) {
	siteList, err := sites.Load(a.cfg.SitesDir())
	if err != nil {
		slog.Warn("не прочитал каталог сайтов", "dir", a.cfg.SitesDir(), "err", err)
	}
	certs, certErr := a.caddy.Certificates()
	alive, aliveMsg := a.caddy.Alive(r.Context())
	publicIP, _ := a.net.PublicIP(r.Context())

	ov := overview{
		Server: serverInfo{
			Hostname: hostname(a.cfg.HostProcDir),
			PublicIP: publicIP,
			Version:  version.Version,
		},
		Sites:       siteList,
		SeenDomains: a.state.Seen(sites.Domains(siteList)),
		Certs:       certs,
		Caddy:       caddyStatus{Alive: alive, Message: aliveMsg},
	}
	fillSystem(&ov.Server, a.cfg.HostProcDir, a.cfg.EdgeDir)

	if certErr != nil {
		ov.Warnings = append(ov.Warnings, warning{
			Level: "yellow", Title: "не вижу сертификаты",
			Detail: certErr.Error(),
			Action: "смонтируйте том caddy_data в панель только на чтение",
		})
	}
	if !alive {
		ov.Warnings = append(ov.Warnings, warning{
			Level: "red", Title: "Caddy не отвечает на admin API",
			Detail: aliveMsg,
			Action: "без него панель не сможет применять изменения",
		})
	}
	for _, c := range certs {
		if c.SelfSign || c.DaysLeft > 21 {
			continue
		}
		level := "yellow"
		if c.DaysLeft <= 7 {
			level = "red"
		}
		ov.Warnings = append(ov.Warnings, warning{
			Level:  level,
			Title:  "сертификат " + strings.Join(c.Domains, ", ") + " истекает через " + itoa(c.DaysLeft) + " дн.",
			Detail: "Caddy обновляет за 30 дней до конца — значит что-то мешает продлению",
			Action: "проверьте, что домен всё ещё смотрит на этот сервер",
		})
	}
	if ports, err := a.ports.List(); err == nil {
		for _, w := range dangerousPorts(ports) {
			ov.Warnings = append(ov.Warnings, w)
		}
	} else {
		ov.Warnings = append(ov.Warnings, warning{
			Level: "yellow", Title: "не вижу порты хоста",
			Detail: err.Error(),
			Action: "смонтируйте /proc хоста в /host/proc только на чтение",
		})
	}
	sort.SliceStable(ov.Warnings, func(i, j int) bool {
		return ov.Warnings[i].Level == "red" && ov.Warnings[j].Level != "red"
	})

	writeJSON(w, http.StatusOK, ov)
}

// dangerousPorts — то, что на старых серверах годами торчало в интернет:
// базы, админки, docker-socket-proxy (DESIGN.md §2.16).
func dangerousPorts(ports []hostports.Port) []warning {
	risky := map[int]string{
		3306: "MySQL", 3307: "MySQL", 3341: "MySQL", 5432: "PostgreSQL",
		6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch",
		2375: "Docker API без TLS", 2376: "Docker API",
		9000: "MinIO / админка", 9001: "агент Portainer", 9443: "Portainer",
		8081: "cAdvisor", 5601: "Kibana", 15672: "RabbitMQ",
	}
	var out []warning
	for _, p := range ports {
		name, bad := risky[p.Port]
		if !bad || p.LoopbackOnly {
			continue
		}
		out = append(out, warning{
			Level:  "red",
			Title:  "порт " + itoa(p.Port) + " (" + name + ") слушает на всех адресах",
			Detail: "держит " + describeProcess(p) + ". Если порт опубликован докером, ufw его не закрывает — фильтр только в цепочке DOCKER-USER",
			Action: "ограничьте доступ: /opt/docker-acl/ports.conf для docker-портов или ufw для хостовых",
		})
	}
	return out
}

func describeProcess(p hostports.Port) string {
	if p.Process == "" {
		return "неизвестный процесс"
	}
	if p.IsDocker {
		return p.Process + " (порт контейнера)"
	}
	return p.Process
}

func (a *app) handleContainers(w http.ResponseWriter, r *http.Request) {
	list, err := a.docker.Containers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	nets, _ := a.docker.Networks(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"containers": list, "networks": nets})
}

func (a *app) handlePorts(w http.ResponseWriter, r *http.Request) {
	list, err := a.ports.List()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"error":     err.Error(),
			"ports":     []hostports.Port{},
		})
		return
	}
	// Сопоставляем порты с контейнерами: docker-proxy сам по себе неинформативен.
	byPublic := map[int]string{}
	if containers, err := a.docker.Containers(r.Context()); err == nil {
		for _, c := range containers {
			for _, p := range c.Ports {
				if p.Published {
					byPublic[p.Public] = c.Name
				}
			}
		}
	}
	type portOut struct {
		hostports.Port
		Container string `json:"container"`
	}
	out := make([]portOut, 0, len(list))
	for _, p := range list {
		out = append(out, portOut{Port: p, Container: byPublic[p.Port]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "ports": out})
}

func (a *app) handleCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := a.caddy.Certificates()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error(), "certificates": []caddyx.Certificate{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"certificates": certs})
}

func (a *app) handleEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"events": a.state.LastEvents(200)})
}

func (a *app) handleCheckDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Domain) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен домен"})
		return
	}
	writeJSON(w, http.StatusOK, a.net.CheckDomain(r.Context(), strings.ToLower(strings.TrimSpace(req.Domain))))
}

func (a *app) handleProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		HostHeader string `json:"host_header"`
	}
	if err := decode(r, &req); err != nil || req.Host == "" || req.Port <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны host и port"})
		return
	}
	writeJSON(w, http.StatusOK, a.net.Probe(r.Context(), req.Host, req.Port, req.HostHeader))
}

func (a *app) handleConnectEdge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Container string `json:"container"`
		Detach    bool   `json:"detach"`
	}
	if err := decode(r, &req); err != nil || req.Container == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужно имя контейнера"})
		return
	}
	var err error
	if req.Detach {
		err = a.docker.DisconnectFromEdge(r.Context(), req.Container)
	} else {
		err = a.docker.ConnectToEdge(r.Context(), req.Container)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	action := "подключён к сети edge"
	if req.Detach {
		action = "отключён от сети edge"
	}
	a.state.Audit("network", clientIP(r), req.Container+" "+action, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": req.Container + " " + action})
}

func (a *app) handleForgetDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := decode(r, &req); err != nil || req.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен домен"})
		return
	}
	a.state.Forget(req.Domain)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- вспомогательное ----

func decode(r *http.Request, out any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func itoa(n int) string { return strconv.Itoa(n) }

// askToken — секрет в пути /internal/tls-ask/<token>. Его знает только Caddy,
// которому его прописал установщик: иначе этот путь можно было бы позвать из
// интернета, ведь панель отвечает на любом ненастроенном домене.
func askToken() string {
	return strings.TrimSpace(os.Getenv("EDGEDECK_TLS_ASK_TOKEN"))
}

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

	"github.com/vladbogun1/edgedeck/internal/apply"
	"github.com/vladbogun1/edgedeck/internal/auth"
	"github.com/vladbogun1/edgedeck/internal/caddyx"
	"github.com/vladbogun1/edgedeck/internal/firewall"
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
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "неверный пароль"})
		}
		return
	}
	a.startSession(w, r, sess)
	a.state.Audit("login", ip, "вход в панель", map[string]any{"host": r.Host})
	a.notifyEvent("login", "🔑 вход в панель с адреса "+ip+"\nдомен: "+r.Host)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.auth.Drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	a.sso.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLogoutAll — выйти везде: все сессии этой панели, все токены единого
// входа на всех серверах флота (сосед получает подписанный сигнал).
func (a *app) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	a.sso.revoke(now)
	a.auth.DropAll()
	failed := a.broadcastRevoke(r, now)
	a.state.Audit("logout_all", clientIP(r), "выход везде", map[string]any{"не_дошло": failed})
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	a.sso.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unreached": failed})
}

// handleSession — единственное, что можно спросить без входа: «я залогинен?».
// Ничего о сервере не рассказывает.
func (a *app) handleSession(w http.ResponseWriter, r *http.Request) {
	_, authed := a.authed(r)
	// Вошли в панель до появления единого входа (или на другом хосте) — выдаём
	// токен, чтобы соседние панели и свои сервисы пускали без повторного входа.
	if authed && a.sso.enabled() && a.sso.inDomain(r.Host) {
		if _, ok := a.sso.fromRequest(r); !ok {
			a.sso.setCookie(w, r)
		}
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
	// В первые минуты после входа текущий пароль не спрашиваем: вход только что
	// подтвердил личность, а пароль мог быть забыт — ради этого и есть вход кодом.
	since, _ := a.authed(r)
	if time.Since(since) >= auth.FreshWindow || req.Current != "" {
		if err := a.auth.Verify(req.Current, ip); err != nil {
			msg := "текущий пароль не подошёл"
			if err == auth.ErrLocked {
				msg = "слишком много попыток — подождите 15 минут"
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": msg})
			return
		}
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
	allowed    bool
	pointsHere bool
	until      time.Time
	reason     string
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
func (l *askLimiter) remember(domain string, allowed, pointsHere bool, reason string) {
	ttl := time.Hour
	if allowed {
		ttl = 5 * time.Minute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache[domain] = askVerdict{allowed: allowed, pointsHere: pointsHere, until: time.Now().Add(ttl), reason: reason}
	if allowed {
		l.granted = append(l.granted, time.Now())
	}
}

// reset забывает закешированные ответы: после одобрения зоны отказ, выданный
// час назад, не должен держать сертификат ещё час.
func (l *askLimiter) reset() {
	l.mu.Lock()
	l.cache = map[string]askVerdict{}
	l.mu.Unlock()
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
		a.state.NoteAsk(domain, v.allowed, v.pointsHere)
		respondAsk(w, v.allowed)
		return
	}
	// Чужая зона: сертификат не выпускаем, даже если A-запись смотрит сюда, —
	// иначе чужой фишинг получает рабочий https на нашем адресе (trust.go).
	// DNS всё равно проверяем: «смотрит сюда» отличает чей-то домен, всерьёз
	// направленный на нас, от сканера с подставленным именем.
	if !a.trust.Trusted(domain) {
		check := a.net.CheckDomain(r.Context(), domain)
		a.tlsAsk.remember(domain, false, check.PointsHere, "чужая зона")
		a.state.NoteAsk(domain, false, check.PointsHere)
		slog.Warn("отказ в сертификате: чужая зона", "domain", domain, "смотрит_сюда", check.PointsHere)
		respondAsk(w, false)
		return
	}
	if a.tlsAsk.overLimit() {
		slog.Warn("отказ в сертификате: лимит новых доменов в час", "domain", domain)
		a.state.NoteAsk(domain, false, false)
		a.state.Audit("tls_ask_denied", "caddy", "лимит новых доменов в час", map[string]any{"domain": domain})
		respondAsk(w, false)
		return
	}

	check := a.net.CheckDomain(r.Context(), domain)
	a.tlsAsk.remember(domain, check.PointsHere, check.PointsHere, check.Message)
	a.state.NoteAsk(domain, check.PointsHere, check.PointsHere)
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
	SeenDomains []seenView           `json:"seen_domains"`
	Certs       []caddyx.Certificate `json:"certificates"`
	Warnings    []warning            `json:"warnings"`
	Caddy       caddyStatus          `json:"caddy"`
	// Drift — routes.yaml или файлы сайтов разошлись с применённым (applied.go).
	Drift *apply.Drift `json:"drift,omitempty"`
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
	writeJSON(w, http.StatusOK, a.buildOverview(r))
}

// buildOverview — сводка сервера: её показывают «Домены» и отдаёт флоту fleet.go.
func (a *app) buildOverview(r *http.Request) overview {
	siteList, err := sites.Load(a.cfg.SitesDir())
	if err != nil {
		slog.Warn("не прочитал каталог сайтов", "dir", a.cfg.SitesDir(), "err", err)
	}
	certs, certErr := a.caddy.Certificates()
	alive, aliveMsg := a.caddy.Alive(r.Context())
	publicIP, _ := a.net.PublicIP(r.Context())

	// Имя сервера спрашиваем у Docker: /proc/sys/kernel/hostname зависит от
	// UTS-namespace и даже через /proc хоста отдаёт имя контейнера.
	name := hostname(a.cfg.HostProcDir)
	if host, err := a.docker.Info(r.Context()); err == nil && host.Name != "" {
		name = host.Name
	}

	ov := overview{
		Server: serverInfo{
			Hostname: name,
			PublicIP: publicIP,
			Version:  version.Version,
		},
		Sites:       siteList,
		SeenDomains: a.seenViews(sites.Domains(siteList)),
		Certs:       certs,
		Caddy:       caddyStatus{Alive: alive, Message: aliveMsg},
	}
	fillSystem(&ov.Server, a.cfg.HostProcDir, a.cfg.EdgeDir)

	if d, err := a.applier.CheckDrift(); err == nil && !d.Empty() {
		ov.Drift = &d
	}

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
		ov.Warnings = append(ov.Warnings, dangerousPorts(ports, a.containerByPort(r))...)
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

	return ov
}

// containerByPort — какой контейнер держит опубликованный порт. Без этого в
// предупреждении будет «неизвестный процесс» вместо имени, потому что порт
// формально держит docker-proxy, а имя процесса панель не читает (DESIGN.md §2.26).
func (a *app) containerByPort(r *http.Request) map[int]string {
	out := map[int]string{}
	containers, err := a.docker.Containers(r.Context())
	if err != nil {
		return out
	}
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.Published {
				out[p.Public] = c.Name
			}
		}
	}
	return out
}

// dangerousPorts — то, что на старых серверах годами торчало в интернет:
// базы, админки, docker-socket-proxy (DESIGN.md §2.16).
func dangerousPorts(ports []hostports.Port, byPort map[int]string) []warning {
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
		holder := byPort[p.Port]
		fromDocker := holder != "" || p.IsDocker
		if holder == "" {
			holder = "процесс на хосте"
			if p.Process != "" {
				holder = p.Process
			}
		}
		w := warning{
			Level: "yellow",
			Title: "порт " + itoa(p.Port) + " (" + name + ") слушает на всех адресах",
		}
		if fromDocker {
			// Docker-порт мимо ufw идёт через FORWARD, и ограничить его можно только
			// в DOCKER-USER. Читать эту цепочку панель пока не умеет (для этого нужен
			// NET_ADMIN), поэтому не пугаем, а говорим как есть.
			w.Detail = "держит " + holder + " (порт контейнера). ufw такие порты не закрывает: они идут мимо INPUT. " +
				"Есть ли ограничение в цепочке DOCKER-USER, панель пока не проверяет"
			w.Action = "если ACL нет — добавьте порт в /opt/docker-acl/ports.conf и примените apply.sh"
		} else {
			w.Level = "red"
			w.Detail = "держит " + holder + " вне докера, то есть порт закрывается обычными правилами ufw"
			w.Action = "ufw deny " + itoa(p.Port) + " или ограничение по адресам, если доступ нужен"
		}
		out = append(out, w)
	}
	return out
}

func (a *app) handleContainers(w http.ResponseWriter, r *http.Request) {
	list, err := a.docker.Containers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	nets, _ := a.docker.Networks(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"containers": list,
		"networks":   nets,
		// Подсказка для чужого compose: подключение сетью панель восстановит сама
		// после пересоздания контейнера, но правильнее закрепить его в файле.
		"compose_hint": composeSnippet(edgeNetworkName()),
	})
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
		Container string           `json:"container"`
		Verdict   firewall.Verdict `json:"verdict"`
	}
	out := make([]portOut, 0, len(list))
	for _, p := range list {
		container := byPublic[p.Port]
		fromDocker := p.IsDocker || container != ""
		out = append(out, portOut{
			Port:      p,
			Container: container,
			Verdict:   a.firewall.Judge(p.Port, p.Proto, fromDocker, p.LoopbackOnly),
		})
	}
	fwOK, fwMsg := a.firewall.Health()
	writeJSON(w, http.StatusOK, map[string]any{
		"available":       true,
		"ports":           out,
		"firewall_ok":     fwOK,
		"firewall_status": fwMsg,
	})
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

// seenView — замеченный домен с пометкой, свой он или чужой (trust.go).
type seenView struct {
	state.SeenDomain
	Zone    string `json:"zone"`
	Foreign bool   `json:"foreign"`
}

func (a *app) seenViews(configured []string) []seenView {
	list := a.state.Seen(configured)
	out := make([]seenView, 0, len(list))
	for _, d := range list {
		out = append(out, seenView{SeenDomain: d, Zone: zoneOf(d.Host), Foreign: !a.trust.Trusted(d.Host)})
	}
	return out
}

// handleTrustZone — кнопка «это мой домен» (или отмена): зона домена становится
// своей, и панель начинает ему отвечать и выпускать сертификаты.
func (a *app) handleTrustZone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain  string `json:"domain"`
		Trusted bool   `json:"trusted"`
	}
	if err := decode(r, &req); err != nil || req.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен домен"})
		return
	}
	zone, err := a.trust.Approve(req.Domain, req.Trusted)
	if zone == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "это не доменное имя"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.tlsAsk.reset()
	msg := "зона " + zone + " отмечена своей"
	if !req.Trusted {
		msg = "зона " + zone + " больше не своя"
	}
	a.state.Audit("trust_zone", clientIP(r), msg, map[string]any{"zone": zone, "trusted": req.Trusted})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "zone": zone, "message": msg})
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

func versionString() string { return version.Version }

// askToken — секрет в пути /internal/tls-ask/<token>. Его знает только Caddy,
// которому его прописал установщик: иначе этот путь можно было бы позвать из
// интернета, ведь панель отвечает на любом ненастроенном домене.
func askToken() string {
	return strings.TrimSpace(os.Getenv("EDGEDECK_TLS_ASK_TOKEN"))
}

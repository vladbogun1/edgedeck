// Package server — HTTP-часть панели.
//
// Особенность, из которой растёт половина решений: панель — обработчик по
// умолчанию для всех доменов без маршрута и для заходов по IP (DESIGN.md §9.0).
// Поэтому форма входа видна посторонним, и до входа наружу не отдаётся ничего:
// ни имени сервера, ни списка доменов, ни версии.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vladbogun1/edgedeck/internal/apply"
	"github.com/vladbogun1/edgedeck/internal/auth"
	"github.com/vladbogun1/edgedeck/internal/caddyx"
	"github.com/vladbogun1/edgedeck/internal/config"
	"github.com/vladbogun1/edgedeck/internal/dockerinfo"
	"github.com/vladbogun1/edgedeck/internal/firewall"
	"github.com/vladbogun1/edgedeck/internal/hostports"
	"github.com/vladbogun1/edgedeck/internal/netcheck"
	"github.com/vladbogun1/edgedeck/internal/notify"
	"github.com/vladbogun1/edgedeck/internal/state"
	"github.com/vladbogun1/edgedeck/internal/webui"
)

const sessionCookie = "edgedeck_session"

type app struct {
	cfg    config.Config
	auth   *auth.Store
	state  *state.Store
	docker *dockerinfo.Client
	caddy  *caddyx.Client
	ports  *hostports.Reader
	net    *netcheck.Checker

	tlsAsk   *askLimiter
	public   *publicCache
	applier  *apply.Applier
	firewall *firewall.Reader
	notify   *notify.Notifier
	watch    *watchState
}

func Run(ctx context.Context, cfg config.Config) error {
	authStore, err := auth.NewStore(cfg.PasswordFile(), cfg.SessionsFile(), cfg.SessionTTL)
	if err != nil {
		return err
	}
	if !authStore.HasPassword() {
		pw := cfg.InitialPassword
		generated := false
		if pw == "" {
			pw = auth.RandomPassword()
			generated = true
		}
		if err := authStore.SetPassword(pw); err != nil {
			return err
		}
		if generated {
			slog.Warn("пароль не был задан — сгенерирован новый",
				"пароль", pw,
				"как_сменить", "войдите в панель и смените пароль в настройках")
		} else {
			slog.Info("пароль установлен из EDGEDECK_INITIAL_PASSWORD")
		}
	}

	a := &app{
		cfg:      cfg,
		auth:     authStore,
		state:    state.NewStore(cfg.SeenDomainsFile(), cfg.AuditFile()),
		docker:   dockerinfo.New(cfg.AgentURL, os.Getenv("EDGEDECK_AGENT_TOKEN"), edgeNetworkName()),
		caddy:    caddyx.New(cfg.CaddyAdminURL, cfg.CaddyDataDir, filepath.Join(cfg.EdgeDir, "certs")),
		ports:    hostports.New(cfg.HostProcDir),
		net:      netcheck.New(cfg.PublicIP),
		tlsAsk:   newAskLimiter(cfg.TLSAskLimit),
		public:   newPublicCache(),
		firewall: firewall.New(cfg.FirewallFile),
		notify:   notify.New(notify.LoadConfig()),
		watch:    newWatchState(),
	}
	a.applier = &apply.Applier{
		EdgeDir:     cfg.EdgeDir,
		SnapshotDir: filepath.Join(cfg.DataDir, "snapshots"),
		Caddy:       a.caddy,
		CaddyHost:   caddyHostName(cfg.CaddyAdminURL),
	}

	mux := http.NewServeMux()
	a.routes(mux)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           a.middleware(mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if a.notify.Enabled() {
		slog.Info("уведомления в Telegram включены")
		go a.runWatcher(ctx)
	} else {
		slog.Info("уведомления в Telegram выключены: нет EDGEDECK_TELEGRAM_TOKEN/CHAT")
	}

	// Замеченные домены сбрасываем на диск по таймеру, а не на каждый запрос.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				a.state.Flush()
				return
			case <-t.C:
				a.state.Flush()
			}
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// Разрешение на выпуск сертификата для незнакомого домена. Спрашивает Caddy,
	// токен в пути — чтобы этот путь нельзя было позвать снаружи (DESIGN.md §7).
	mux.HandleFunc("GET /internal/tls-ask/{token}", a.handleTLSAsk)

	// Витрина сайтов — до всякого входа (DESIGN.md §9.1).
	mux.HandleFunc("GET /api/public/sites", a.handlePublicSites)

	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/session", a.handleSession)

	mux.HandleFunc("GET /api/overview", a.requireAuth(a.handleOverview))
	mux.HandleFunc("GET /api/containers", a.requireAuth(a.handleContainers))
	mux.HandleFunc("GET /api/ports", a.requireAuth(a.handlePorts))
	mux.HandleFunc("GET /api/certificates", a.requireAuth(a.handleCertificates))
	mux.HandleFunc("GET /api/events", a.requireAuth(a.handleEvents))
	mux.HandleFunc("POST /api/check-domain", a.requireAuth(a.handleCheckDomain))
	mux.HandleFunc("POST /api/probe", a.requireAuth(a.handleProbe))
	mux.HandleFunc("POST /api/connect-edge", a.requireAuth(a.handleConnectEdge))
	mux.HandleFunc("POST /api/forget-domain", a.requireAuth(a.handleForgetDomain))
	mux.HandleFunc("POST /api/password", a.requireAuth(a.handleChangePassword))

	// Маршруты. Любая запись идёт только через транзакцию с откатом (DESIGN.md §5.2).
	mux.HandleFunc("GET /api/routes", a.requireAuth(a.handleRoutesList))
	mux.HandleFunc("POST /api/routes/save", a.requireAuth(a.handleRouteSave))
	mux.HandleFunc("POST /api/routes/delete", a.requireAuth(a.handleRouteDelete))
	mux.HandleFunc("POST /api/routes/import", a.requireAuth(a.handleRouteImport))
	mux.HandleFunc("GET /api/snapshots", a.requireAuth(a.handleSnapshots))
	mux.HandleFunc("GET /api/settings", a.requireAuth(a.handleSettings))
	mux.HandleFunc("POST /api/settings/test-telegram", a.requireAuth(a.handleTestTelegram))
	mux.HandleFunc("POST /api/rollback", a.requireAuth(a.handleRollback))

	mux.Handle("/", webui.Handler())
}

// middleware отмечает каждый Host, который к нам пришёл: именно так панель
// узнаёт про домены, у которых ещё нет маршрута.
func (a *app) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/internal/") && !strings.HasPrefix(r.URL.Path, "/healthz") {
			a.state.NoteHost(r.Host, false, false)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "нужен вход"})
			return
		}
		if _, ok := a.auth.Check(c.Value); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "сессия истекла"})
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	// Панель всегда за Caddy, поэтому доверяем X-Forwarded-For: наружу порт не
	// опубликован, напрямую до панели дойти нельзя (DESIGN.md §10).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// caddyHostName вытаскивает имя контейнера прокси из адреса admin API:
// «http://edge_caddy:2019» → «edge_caddy». Через это имя идут проверки доменов.
func caddyHostName(adminURL string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(adminURL, "http://"), "https://")
	if i := strings.Index(s, ":"); i > 0 {
		s = s[:i]
	}
	if s == "" {
		return "edge_caddy"
	}
	return s
}

func edgeNetworkName() string {
	if v := strings.TrimSpace(os.Getenv("EDGEDECK_EDGE_NETWORK")); v != "" {
		return v
	}
	return "edge"
}

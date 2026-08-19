package server

import (
	"net/http"

	"github.com/vladbogun1/edgedeck/internal/auth"
)

// Настройки: то немногое, что панель показывает про саму себя.
//
// Секретов здесь нет: ни токена бота, ни пароля, ни токена агента. Только
// «настроено / не настроено» и состояние подсистем — иначе экран настроек стал бы
// удобным местом, откуда всё это утекает.

type settingsResponse struct {
	Server struct {
		Hostname string `json:"hostname"`
		PublicIP string `json:"public_ip"`
		Version  string `json:"version"`
		EdgeDir  string `json:"edge_dir"`
	} `json:"server"`
	Telegram struct {
		Configured bool `json:"configured"`
	} `json:"telegram"`
	Firewall struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		UFWWork bool   `json:"ufw_works"`
	} `json:"firewall"`
	Caddy struct {
		Alive   bool   `json:"alive"`
		Message string `json:"message"`
	} `json:"caddy"`
	Agent struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	} `json:"agent"`
	Sessions []auth.Session `json:"sessions"`
	Watch    struct {
		Interval string `json:"interval"`
		DownWarn string `json:"down_warn"`
		CertWarn int    `json:"cert_warn_days"`
	} `json:"watch"`
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request) {
	var resp settingsResponse

	resp.Server.Hostname = hostname(a.cfg.HostProcDir)
	if host, err := a.docker.Info(r.Context()); err == nil {
		if host.Name != "" {
			resp.Server.Hostname = host.Name
		}
		resp.Agent.OK = true
		resp.Agent.Message = "docker " + host.DockerVersion + ", " + host.Architecture
	} else {
		resp.Agent.Message = err.Error()
	}
	resp.Server.PublicIP, _ = a.net.PublicIP(r.Context())
	resp.Server.Version = versionString()
	resp.Server.EdgeDir = a.cfg.EdgeDir

	resp.Telegram.Configured = a.notify.Enabled()

	ok, status := a.firewall.Health()
	dump, _ := a.firewall.Dump()
	resp.Firewall.OK = ok
	resp.Firewall.Status = status
	resp.Firewall.UFWWork = dump.UFWWorks

	alive, msg := a.caddy.Alive(r.Context())
	resp.Caddy.Alive = alive
	resp.Caddy.Message = msg

	resp.Sessions = a.auth.Sessions()
	resp.Watch.Interval = "1 минута"
	resp.Watch.DownWarn = "5 минут"
	resp.Watch.CertWarn = certWarnDays

	writeJSON(w, http.StatusOK, resp)
}

// handleTestTelegram отправляет пробное сообщение. Полезно ровно потому, что
// «настроено» и «работает» — разные вещи, и выяснять это лучше не в аварии.
func (a *app) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	host := hostname(a.cfg.HostProcDir)
	if err := a.notify.Send(r.Context(), "✅ EdgeDeck на <b>"+host+"</b>: связь есть.\nСюда будут приходить сообщения о недоступных сайтах, кончающихся сертификатах и новых доменах."); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.state.Audit("telegram_test", clientIP(r), "отправил пробное сообщение в Telegram", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "отправил — проверьте чат"})
}

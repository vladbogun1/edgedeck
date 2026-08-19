// Package config — настройки панели.
//
// Всё берётся из переменных окружения (их задаёт docker-compose), кроме пароля:
// его хеш живёт в файле внутри тома данных, чтобы смена пароля из интерфейса
// не требовала правки compose и перезапуска.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	// Listen — адрес панели внутри docker-сети. Наружу не публикуется никогда:
	// снаружи в панель попадают только через Caddy (DESIGN.md §10).
	Listen string

	// EdgeDir — каталог Caddy (~/edge на сервере), смонтирован на запись.
	// Отсюда читаются sites/*.caddy, сюда же будет писаться routes.yaml.
	EdgeDir string

	// DataDir — состояние самой панели: хеш пароля, сессии, журнал действий.
	DataDir string

	// AgentURL — сайдкар с доступом к Docker.
	AgentURL string

	// CaddyAdminURL — admin API Caddy внутри сети edge.
	CaddyAdminURL string

	// CaddyDataDir — том caddy_data, смонтированный только на чтение.
	// Нужен, чтобы читать срок действия сертификатов, не спрашивая никого.
	CaddyDataDir string

	// HostProcDir — /proc хоста только на чтение: так находятся порты
	// процессов вне докера (DESIGN.md §6.1).
	HostProcDir string

	// PublicIP — внешний адрес сервера. Если пусто, определяется сам.
	PublicIP string

	// FirewallFile — снимок правил фаервола, который пишет отдельный контейнер.
	FirewallFile string

	// TLSAskLimit — сколько новых доменов в час разрешаем выпускать (DESIGN.md §7).
	TLSAskLimit int

	// SessionTTL — срок жизни сессии.
	SessionTTL time.Duration

	// InitialPassword — если хеша пароля ещё нет, панель создаст его из этого
	// значения при первом старте. Иначе сгенерирует случайный и напишет в лог.
	InitialPassword string
}

func Load() (Config, error) {
	c := Config{
		Listen:          env("EDGEDECK_LISTEN", ":4000"),
		EdgeDir:         env("EDGEDECK_EDGE_DIR", "/edge"),
		DataDir:         env("EDGEDECK_DATA_DIR", "/data"),
		AgentURL:        strings.TrimRight(env("EDGEDECK_AGENT_URL", "http://edgedeck-agent:4001"), "/"),
		CaddyAdminURL:   strings.TrimRight(env("EDGEDECK_CADDY_ADMIN", "http://edge_caddy:2019"), "/"),
		CaddyDataDir:    env("EDGEDECK_CADDY_DATA", "/caddy-data"),
		HostProcDir:     env("EDGEDECK_HOST_PROC", "/host/proc"),
		PublicIP:        env("EDGEDECK_PUBLIC_IP", ""),
		FirewallFile:    env("EDGEDECK_FIREWALL_FILE", "/state/firewall.json"),
		TLSAskLimit:     envInt("EDGEDECK_TLS_ASK_LIMIT", 10),
		SessionTTL:      envDuration("EDGEDECK_SESSION_TTL", 30*24*time.Hour),
		InitialPassword: env("EDGEDECK_INITIAL_PASSWORD", ""),
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return c, fmt.Errorf("каталог данных %s: %w", c.DataDir, err)
	}
	return c, nil
}

// PasswordFile — файл с хешем пароля (argon2id), режим 0600.
func (c Config) PasswordFile() string { return filepath.Join(c.DataDir, "password") }

// SessionsFile — активные сессии. В файле, а не в памяти, чтобы обновление
// образа не выкидывало из панели.
func (c Config) SessionsFile() string { return filepath.Join(c.DataDir, "sessions.json") }

// SeenDomainsFile — домены, которые пришли на сервер, но маршрута не имеют.
// Панель узнаёт о них сама: она и есть обработчик по умолчанию (DESIGN.md §9.0).
func (c Config) SeenDomainsFile() string { return filepath.Join(c.DataDir, "seen-domains.json") }

// AuditFile — журнал действий, по строке JSON на событие.
func (c Config) AuditFile() string { return filepath.Join(c.DataDir, "audit.jsonl") }

// SitesDir — каталог конфигов сайтов Caddy.
func (c Config) SitesDir() string { return filepath.Join(c.EdgeDir, "sites") }

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := env(key, "")
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := env(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

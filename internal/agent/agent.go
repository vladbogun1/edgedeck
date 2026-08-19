// Package agent — сайдкар, через который панель разговаривает с Docker.
//
// Зачем он вообще: docker.sock — это root на сервере. Готовый
// tecnativa/docker-socket-proxy пришлось бы запускать с POST=1, чтобы разрешить
// подключение контейнера к сети, а это заодно открывает создание и запуск
// контейнеров. Поэтому здесь белый список из шести операций и ничего больше
// (DESIGN.md §4).
//
// Агент слушает внутри сети edge, где живут и чужие контейнеры, поэтому запросы
// подписаны общим секретом: без заголовка X-EdgeDeck-Token отвечает 401.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Listen       string
	DockerSocket string
	EdgeNetwork  string
	Token        string
}

func LoadConfig() Config {
	return Config{
		Listen:       envOr("EDGEDECK_AGENT_LISTEN", ":4001"),
		DockerSocket: envOr("EDGEDECK_DOCKER_SOCKET", "/var/run/docker.sock"),
		EdgeNetwork:  envOr("EDGEDECK_EDGE_NETWORK", "edge"),
		Token:        os.Getenv("EDGEDECK_AGENT_TOKEN"),
	}
}

// idRe — то, что мы согласны передать в путь запроса к Docker.
// Имена контейнеров и сетей у докера ограничены этим набором, так что
// экзотику отсекаем сразу, не полагаясь на экранирование ниже.
var idRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type server struct {
	cfg    Config
	docker *http.Client
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.Token == "" {
		return errors.New("EDGEDECK_AGENT_TOKEN не задан: агент в сети edge без секрета — открытая дверь")
	}
	if _, err := os.Stat(cfg.DockerSocket); err != nil {
		return fmt.Errorf("docker.sock (%s): %w", cfg.DockerSocket, err)
	}

	s := &server{
		cfg: cfg,
		docker: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", cfg.DockerSocket)
				},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /v1/containers", s.auth(s.containers))
	mux.HandleFunc("GET /v1/containers/{id}", s.auth(s.container))
	mux.HandleFunc("GET /v1/networks", s.auth(s.networks))
	mux.HandleFunc("GET /v1/info", s.auth(s.info))
	mux.HandleFunc("POST /v1/edge-network/connect", s.auth(s.connect))
	mux.HandleFunc("POST /v1/edge-network/disconnect", s.auth(s.disconnect))
	// Всё остальное — явный отказ, чтобы в логе было видно попытку.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("агент: запрос вне белого списка", "method", r.Method, "path", r.URL.Path, "from", r.RemoteAddr)
		http.Error(w, "не разрешено", http.StatusForbidden)
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-EdgeDeck-Token") != s.cfg.Token {
			http.Error(w, "нужен токен", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *server) containers(w http.ResponseWriter, r *http.Request) {
	s.passthrough(w, r, "/containers/json?all=1")
}

func (s *server) container(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		http.Error(w, "плохой идентификатор", http.StatusBadRequest)
		return
	}
	s.passthrough(w, r, "/containers/"+url.PathEscape(id)+"/json")
}

func (s *server) networks(w http.ResponseWriter, r *http.Request) {
	s.passthrough(w, r, "/networks")
}

func (s *server) info(w http.ResponseWriter, r *http.Request) {
	s.passthrough(w, r, "/info")
}

type connectRequest struct {
	Container string `json:"container"`
}

func (s *server) connect(w http.ResponseWriter, r *http.Request)    { s.attach(w, r, "connect") }
func (s *server) disconnect(w http.ResponseWriter, r *http.Request) { s.attach(w, r, "disconnect") }

// attach — единственная операция записи, которую агент умеет: подключить или
// отключить контейнер от сети edge. Имя сети берётся из конфигурации агента,
// а не из запроса, поэтому панель не может тронуть никакую другую сеть.
func (s *server) attach(w http.ResponseWriter, r *http.Request, action string) {
	var req connectRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "не разобрал запрос", http.StatusBadRequest)
		return
	}
	if !idRe.MatchString(req.Container) {
		http.Error(w, "плохое имя контейнера", http.StatusBadRequest)
		return
	}
	body, _ := json.Marshal(map[string]string{"Container": req.Container})
	path := "/networks/" + url.PathEscape(s.cfg.EdgeNetwork) + "/" + action

	resp, err := s.do(r.Context(), http.MethodPost, path, strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	slog.Info("агент: сеть edge", "action", action, "container", req.Container, "status", resp.StatusCode)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}

func (s *server) passthrough(w http.ResponseWriter, r *http.Request, path string) {
	resp, err := s.do(r.Context(), http.MethodGet, path, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
}

func (s *server) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.docker.Do(req)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

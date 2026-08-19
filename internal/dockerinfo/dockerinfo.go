// Package dockerinfo — что панель знает о контейнерах.
//
// Панель не разговаривает с docker.sock напрямую: только через агента
// (см. internal/agent). Здесь разбор ответов и превращение их в «кандидатов»,
// которые видит пользователь в визарде.
package dockerinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Client struct {
	base        string
	token       string
	edgeNetwork string
	http        *http.Client
}

func New(base, token, edgeNetwork string) *Client {
	return &Client{
		base:        strings.TrimRight(base, "/"),
		token:       token,
		edgeNetwork: edgeNetwork,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

// ---- то, что отдаёт Docker (только нужные поля) ----

type rawContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []rawPort         `json:"Ports"`
	NetSet struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type rawPort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// ---- то, что видит панель ----

// Container — контейнер как кандидат для маршрута.
type Container struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	State    string            `json:"state"`  // running / exited / ...
	Status   string            `json:"status"` // «Up 3 days»
	Networks []string          `json:"networks"`
	InEdge   bool              `json:"in_edge"`
	Ports    []ContainerPort   `json:"ports"`
	Compose  ComposeInfo       `json:"compose"`
	Labels   map[string]string `json:"-"`

	// Suggested — порт, который панель предлагает по умолчанию (DESIGN.md §6.3).
	Suggested int `json:"suggested_port"`

	// Reachability — что нужно сделать, чтобы прокси до него дошёл.
	Reachability string `json:"reachability"` // ready | needs_edge | host_network | not_running
	Hint         string `json:"hint"`
}

type ContainerPort struct {
	Private   int    `json:"private"`
	Public    int    `json:"public"`
	Type      string `json:"type"`
	BindIP    string `json:"bind_ip"`
	Published bool   `json:"published"`
	// LoopbackOnly — опубликован на 127.0.0.1: с хоста доступен, из другого
	// контейнера — нет (DESIGN.md §2.4). Маршрутизировать надо по docker-сети.
	LoopbackOnly bool `json:"loopback_only"`
}

type ComposeInfo struct {
	Project    string `json:"project"`
	Service    string `json:"service"`
	WorkingDir string `json:"working_dir"`
}

// Network — docker-сеть.
type Network struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Subnet string `json:"subnet"`
}

func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	var raw []rawContainer
	if err := c.get(ctx, "/v1/containers", &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, c.convert(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	var raw []struct {
		Name   string `json:"Name"`
		Driver string `json:"Driver"`
		IPAM   struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			} `json:"Config"`
		} `json:"IPAM"`
	}
	if err := c.get(ctx, "/v1/networks", &raw); err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(raw))
	for _, r := range raw {
		n := Network{Name: r.Name, Driver: r.Driver}
		if len(r.IPAM.Config) > 0 {
			n.Subnet = r.IPAM.Config[0].Subnet
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ConnectToEdge подключает контейнер к сети edge — та единственная запись,
// которую панель себе позволяет (DESIGN.md §2.3).
func (c *Client) ConnectToEdge(ctx context.Context, container string) error {
	return c.post(ctx, "/v1/edge-network/connect", map[string]string{"container": container})
}

func (c *Client) DisconnectFromEdge(ctx context.Context, container string) error {
	return c.post(ctx, "/v1/edge-network/disconnect", map[string]string{"container": container})
}

func (c *Client) convert(r rawContainer) Container {
	out := Container{
		Name:   strings.TrimPrefix(firstOr(r.Names, ""), "/"),
		Image:  r.Image,
		State:  r.State,
		Status: r.Status,
		Labels: r.Labels,
		Compose: ComposeInfo{
			Project:    r.Labels["com.docker.compose.project"],
			Service:    r.Labels["com.docker.compose.service"],
			WorkingDir: r.Labels["com.docker.compose.project.working_dir"],
		},
	}
	for name := range r.NetSet.Networks {
		out.Networks = append(out.Networks, name)
		if name == c.edgeNetwork {
			out.InEdge = true
		}
	}
	sort.Strings(out.Networks)

	seen := map[string]bool{}
	for _, p := range r.Ports {
		key := fmt.Sprintf("%d/%s/%s", p.PrivatePort, p.Type, p.IP)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Ports = append(out.Ports, ContainerPort{
			Private:      p.PrivatePort,
			Public:       p.PublicPort,
			Type:         p.Type,
			BindIP:       p.IP,
			Published:    p.PublicPort != 0,
			LoopbackOnly: strings.HasPrefix(p.IP, "127."),
		})
	}
	sort.Slice(out.Ports, func(i, j int) bool { return out.Ports[i].Private < out.Ports[j].Private })

	out.Suggested = suggestPort(out)
	out.Reachability, out.Hint = reachability(out, c.edgeNetwork)
	return out
}

// suggestPort — догадка о рабочем порте. Порядок предпочтений взят из того, что
// реально встречается на серверах: nginx-образы (80), фронтенды на 3000,
// бэкенды на 8080/4000.
func suggestPort(c Container) int {
	preferred := []int{80, 8080, 3000, 4000, 8000, 5000, 8081, 5173}
	tcp := map[int]bool{}
	for _, p := range c.Ports {
		if p.Type == "tcp" {
			tcp[p.Private] = true
		}
	}
	if len(tcp) == 1 {
		for p := range tcp {
			return p
		}
	}
	for _, p := range preferred {
		if tcp[p] {
			return p
		}
	}
	best := 0
	for p := range tcp {
		if best == 0 || p < best {
			best = p
		}
	}
	return best
}

func reachability(c Container, edgeNetwork string) (string, string) {
	if c.State != "running" {
		return "not_running", "контейнер не запущен — маршрут создать можно, но отвечать он начнёт только после запуска"
	}
	if c.InEdge {
		return "ready", ""
	}
	if len(c.Networks) == 0 {
		return "host_network", "контейнер вне docker-сетей (host или none): доступен только через порт хоста, а на этом сервере это отдельная история — см. подсказку про фаервол"
	}
	return "needs_edge", "контейнер не в сети " + edgeNetwork + " — подключу его одной кнопкой, чужой docker-compose.yml при этом не тронут"
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-EdgeDeck-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("агент недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("агент ответил %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-EdgeDeck-Token", c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("агент недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("агент ответил %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}
	return def
}

// Host — сведения о сервере от самого Docker.
//
// Нужны из-за неочевидного: /proc/sys/kernel/hostname зависит от UTS-namespace,
// поэтому даже через смонтированный /proc хоста контейнер читает СВОЁ имя.
// Docker же знает настоящее имя машины.
type Host struct {
	Name          string `json:"name"`
	CPUs          int    `json:"cpus"`
	MemTotalMB    int    `json:"mem_total_mb"`
	DockerVersion string `json:"docker_version"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
}

func (c *Client) Info(ctx context.Context) (Host, error) {
	var raw struct {
		Name          string `json:"Name"`
		NCPU          int    `json:"NCPU"`
		MemTotal      int64  `json:"MemTotal"`
		ServerVersion string `json:"ServerVersion"`
		OperatingSyst string `json:"OperatingSystem"`
		Architecture  string `json:"Architecture"`
	}
	if err := c.get(ctx, "/v1/info", &raw); err != nil {
		return Host{}, err
	}
	return Host{
		Name:          raw.Name,
		CPUs:          raw.NCPU,
		MemTotalMB:    int(raw.MemTotal / (1024 * 1024)),
		DockerVersion: raw.ServerVersion,
		OS:            raw.OperatingSyst,
		Architecture:  raw.Architecture,
	}, nil
}

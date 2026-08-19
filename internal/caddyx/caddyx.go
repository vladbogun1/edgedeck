// Package caddyx — разговор с Caddy.
//
// Две вещи:
//  1. admin API (/adapt для проверки, /load для применения без разрыва
//     соединений). Никакого docker exec: панели не нужны права на запуск команд
//     в чужих контейнерах.
//  2. сертификаты — читаются прямо из тома caddy_data, смонтированного на
//     чтение. Так срок годности виден, даже если admin API недоступен.
package caddyx

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Client struct {
	adminURL string
	dataDir  string
	// certsDir — каталог явных сертификатов-файлов (~/edge/certs). Там лежит
	// самоподписанный сертификат для захода по IP: Let's Encrypt на адреса их
	// не выдаёт (DESIGN.md §2.22), поэтому в caddy_data его нет.
	certsDir string
	http     *http.Client
}

func New(adminURL, dataDir, certsDir string) *Client {
	return &Client{
		adminURL: strings.TrimRight(adminURL, "/"),
		dataDir:  dataDir,
		certsDir: certsDir,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Alive — отвечает ли admin API. Показывается на главном экране: если нет,
// значит панель не сможет применять изменения, и лучше сказать об этом сразу.
func (c *Client) Alive(ctx context.Context) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.adminURL+"/config/apps/http/servers", nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, "admin API Caddy недоступен: " + err.Error() +
			" — проверьте, что в Caddyfile стоит `admin :2019`, а не `admin localhost:2019`"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("admin API ответил %d", resp.StatusCode)
	}
	return true, ""
}

// Validate прогоняет Caddyfile через /adapt. Это проверка до применения:
// битый конфиг не должен даже приближаться к работающему прокси (DESIGN.md §5.2).
func (c *Client) Validate(ctx context.Context, caddyfile []byte) error {
	resp, err := c.post(ctx, "/adapt", "text/caddyfile", caddyfile)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("конфиг не прошёл проверку: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

// Load применяет конфиг. Caddy подхватывает его без разрыва соединений.
func (c *Client) Load(ctx context.Context, caddyfile []byte) error {
	resp, err := c.post(ctx, "/load", "text/caddyfile", caddyfile)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("конфиг не применился: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) post(ctx context.Context, path, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.adminURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.http.Do(req)
}

// Certificate — сертификат из тома caddy_data.
type Certificate struct {
	Domains   []string  `json:"domains"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	DaysLeft  int       `json:"days_left"`
	SelfSign  bool      `json:"self_signed"`
	Path      string    `json:"-"`
}

// Certificates обходит /caddy-data/caddy/certificates/**/*.crt.
// Каталог смонтирован только на чтение: удалить сертификаты панель не может
// физически — у Let's Encrypt лимит на повторный выпуск (DESIGN.md §2.8).
func (c *Client) Certificates() ([]Certificate, error) {
	var out []Certificate
	if c.certsDir != "" {
		if entries, derr := os.ReadDir(c.certsDir); derr == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
					continue
				}
				if cert, perr := parseCert(filepath.Join(c.certsDir, e.Name())); perr == nil {
					cert.SelfSign = true
					out = append(out, cert)
				}
			}
		}
	}
	// Явные сертификаты уже собраны; том Caddy может быть и не смонтирован —
	// тогда отдаём то, что есть, вместе с объяснением.
	root := filepath.Join(c.dataDir, "caddy", "certificates")
	if _, statErr := os.Stat(root); statErr != nil {
		return out, fmt.Errorf("том с сертификатами Caddy не смонтирован (%s)", root)
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".crt") {
			return nil
		}
		cert, perr := parseCert(path)
		if perr != nil {
			return nil // повреждённый файл не должен ломать весь список
		}
		out = append(out, cert)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out, err
}

func parseCert(path string) (Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Certificate{}, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return Certificate{}, fmt.Errorf("не PEM: %s", path)
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Certificate{}, err
	}
	issuer := x.Issuer.CommonName
	if issuer == "" && len(x.Issuer.Organization) > 0 {
		issuer = x.Issuer.Organization[0]
	}
	domains := append([]string{}, x.DNSNames...)
	for _, ip := range x.IPAddresses {
		domains = append(domains, ip.String())
	}
	if len(domains) == 0 {
		domains = []string{x.Subject.CommonName}
	}
	return Certificate{
		Domains:   domains,
		Issuer:    issuer,
		NotBefore: x.NotBefore,
		NotAfter:  x.NotAfter,
		DaysLeft:  int(time.Until(x.NotAfter).Hours() / 24),
		SelfSign:  strings.Contains(strings.ToLower(issuer), "caddy local"),
		Path:      path,
	}, nil
}

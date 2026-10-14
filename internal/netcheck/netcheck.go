// Package netcheck — проверки «а дойдёт ли».
//
// Главная из них: смотрит ли домен на этот сервер. От неё зависит, разрешать ли
// выпуск сертификата (DESIGN.md §7) — без такой проверки любой чужой домен,
// направленный на наш IP, будет жечь лимиты Let's Encrypt.
package netcheck

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Checker struct {
	resolver *net.Resolver
	http     *http.Client

	forcedIP string

	mu       sync.Mutex
	publicIP string
	fetched  time.Time
}

func New(forcedIP string) *Checker {
	return &Checker{
		resolver: &net.Resolver{},
		http:     &http.Client{Timeout: 8 * time.Second},
		forcedIP: strings.TrimSpace(forcedIP),
	}
}

// PublicIP — внешний адрес сервера. У Oracle он за NAT, поэтому по интерфейсам
// его не увидеть: локально там 10.0.0.x. Значит либо задан руками в EDGEDECK_PUBLIC_IP,
// либо спрашиваем у внешнего сервиса и держим в памяти час.
func (c *Checker) PublicIP(ctx context.Context) (string, error) {
	if c.forcedIP != "" {
		return c.forcedIP, nil
	}
	c.mu.Lock()
	if c.publicIP != "" && time.Since(c.fetched) < time.Hour {
		ip := c.publicIP
		c.mu.Unlock()
		return ip, nil
	}
	c.mu.Unlock()

	var lastErr error
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://icanhazip.com"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		ip := strings.TrimSpace(string(b))
		if net.ParseIP(ip) == nil {
			lastErr = fmt.Errorf("%s вернул не адрес: %q", url, ip)
			continue
		}
		c.mu.Lock()
		c.publicIP, c.fetched = ip, time.Now()
		c.mu.Unlock()
		return ip, nil
	}
	return "", fmt.Errorf("не удалось узнать внешний адрес: %w", lastErr)
}

// DomainCheck — результат проверки домена.
type DomainCheck struct {
	Domain     string   `json:"domain"`
	Resolved   []string `json:"resolved"`
	ServerIP   string   `json:"server_ip"`
	PointsHere bool     `json:"points_here"`
	Message    string   `json:"message"`
}

// CheckDomain отвечает на вопрос «эта A-запись про нас?» человеческим языком.
func (c *Checker) CheckDomain(ctx context.Context, domain string) DomainCheck {
	out := DomainCheck{Domain: domain}
	serverIP, err := c.PublicIP(ctx)
	if err != nil {
		out.Message = "не смог определить внешний адрес сервера: " + err.Error() +
			" — задайте EDGEDECK_PUBLIC_IP в .env"
		return out
	}
	out.ServerIP = serverIP

	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := c.resolver.LookupHost(lookupCtx, domain)
	if err != nil {
		out.Message = "домен не резолвится: " + cleanDNSError(err) +
			". Добавьте A-запись " + domain + " → " + serverIP
		return out
	}
	sort.Strings(addrs)
	out.Resolved = addrs
	for _, a := range addrs {
		if a == serverIP {
			out.PointsHere = true
			out.Message = "домен смотрит сюда — сертификат выпустится"
			return out
		}
	}
	out.Message = "домен смотрит на " + strings.Join(addrs, ", ") + ", а этот сервер — " + serverIP +
		". Пока A-запись не переедет, сертификат выпустить нельзя"
	return out
}

// WildcardHere — отвечает ли на наш адрес и соседнее имя, которого никто не
// заводил (случайная метка на месте первой). Если да, A-запись домена ничего не
// доказывает: её синтезирует wildcard (*.зона или сервис вроде sslip.io), и
// сканер может придумать сколько угодно таких имён. Именно так возникает петля
// «выпустили сертификат → имя попало в CT-логи → сканер пробует www.<имя> →
// выпустили ещё один». Ошибка резолва — «нет», зона без wildcard так и выглядит.
func (c *Checker) WildcardHere(ctx context.Context, domain string) bool {
	domain = strings.Trim(strings.ToLower(domain), ".")
	i := strings.IndexByte(domain, '.')
	if i < 0 || !strings.Contains(domain[i+1:], ".") {
		return false // вершина зоны: соседа не придумать
	}
	serverIP, err := c.PublicIP(ctx)
	if err != nil {
		return false
	}
	var rnd [5]byte
	_, _ = rand.Read(rnd[:])
	probe := "edgedeck-probe-" + hex.EncodeToString(rnd[:]) + domain[i:]

	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := c.resolver.LookupHost(lookupCtx, probe)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a == serverIP {
			return true
		}
	}
	return false
}

// ProbeResult — результат стука в цель из сети edge.
type ProbeResult struct {
	Target     string        `json:"target"`
	TCPOpen    bool          `json:"tcp_open"`
	HTTPStatus int           `json:"http_status"`
	Server     string        `json:"server"`
	Redirect   string        `json:"redirect"`
	HasCSP     bool          `json:"has_csp"`
	HasHSTS    bool          `json:"has_hsts"`
	Duration   time.Duration `json:"-"`
	DurationMS int64         `json:"duration_ms"`
	Message    string        `json:"message"`
}

// Probe стучится в цель ровно оттуда, откуда потом пойдёт Caddy — из сети edge.
// Это та самая предполётная проверка из DESIGN.md §6.2.
func (c *Checker) Probe(ctx context.Context, host string, port int, hostHeader string) ProbeResult {
	addr := fmt.Sprintf("%s:%d", host, port)
	res := ProbeResult{Target: addr}
	start := time.Now()

	dialCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		res.DurationMS = time.Since(start).Milliseconds()
		res.Message = explainDialError(host, err)
		return res
	}
	_ = conn.Close()
	res.TCPOpen = true

	client := &http.Client{
		Timeout: 6 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // редирект не идём: он сам по себе информация
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		res.Message = "порт открыт, но запрос не собрался: " + err.Error()
		return res
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := client.Do(req)
	res.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Message = "порт открыт, но это не HTTP (или ответ не дождались): " + err.Error() +
			". Для игровых серверов, VPN и баз это нормально — им нужен маршрут по порту, а не по домену"
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	res.HTTPStatus = resp.StatusCode
	res.Server = resp.Header.Get("Server")
	res.Redirect = resp.Header.Get("Location")
	res.HasCSP = resp.Header.Get("Content-Security-Policy") != ""
	res.HasHSTS = resp.Header.Get("Strict-Transport-Security") != ""

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.Message = fmt.Sprintf("отвечает %d — годится", resp.StatusCode)
	case resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 308:
		if strings.HasPrefix(strings.ToLower(res.Redirect), "https://") {
			res.Message = "цель сама редиректит на https — если проксировать как есть, получится петля. " +
				"Обычно у приложения есть настройка «доверять X-Forwarded-Proto», её и надо включить"
		} else {
			res.Message = fmt.Sprintf("отвечает %d на %s — так тоже бывает нормально", resp.StatusCode, res.Redirect)
		}
	case resp.StatusCode == 404:
		res.Message = "порт живой, но корень отдаёт 404 — возможно, приложение ждёт свой путь или свой Host"
	default:
		res.Message = fmt.Sprintf("отвечает %d", resp.StatusCode)
	}
	return res
}

func explainDialError(host string, err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "имя " + host + " не разрешается внутри сети edge — обычно значит, что контейнер не подключён к этой сети"
	case strings.Contains(s, "connection refused"):
		return "соединение отклонено: адрес есть, но порт закрыт — скорее всего порт не тот"
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "context deadline"):
		return "нет ответа: пакеты уходят в никуда. Если цель — процесс на хосте, дело почти наверняка в порядке правил iptables (см. DESIGN.md §2.6)"
	default:
		return "не подключился: " + s
	}
}

func cleanDNSError(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 {
		return s[i+2:]
	}
	return s
}

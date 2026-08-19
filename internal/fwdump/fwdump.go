// Package fwdump — снимок правил фаервола для панели.
//
// Зачем отдельный режим бинарника. Чтобы ответить на вопрос «этот порт видно из
// интернета?», нужно прочитать цепочку DOCKER-USER: ufw docker-порты не
// фильтрует вообще (DESIGN.md §2.16). Читать iptables можно только с
// CAP_NET_ADMIN и в сетевом пространстве хоста — прав, которых панели давать
// нельзя.
//
// Поэтому права выделены в отдельный контейнер, который:
//   - умеет ровно три чтения и ни одной записи;
//   - никуда не слушает (нет порта — нет и способа его попросить о чём-то);
//   - раз в 30 секунд кладёт результат в файл, а панель этот файл читает.
//
// Худшее, что может сделать взломанная панель через этот канал — прочитать
// устаревший дамп правил.
package fwdump

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	OutFile  string
	UFWRules string
	Interval time.Duration
}

func LoadConfig() Config {
	return Config{
		OutFile:  envOr("EDGEDECK_FIREWALL_FILE", "/state/firewall.json"),
		UFWRules: envOr("EDGEDECK_UFW_RULES", "/host/etc/ufw/user.rules"),
		Interval: 30 * time.Second,
	}
}

// Dump — то, что панель получает на вход.
type Dump struct {
	Time time.Time `json:"time"`
	// DockerUser — правила цепочки DOCKER-USER: единственное место, где можно
	// ограничить порт, опубликованный докером.
	DockerUser []string `json:"docker_user"`
	// Input — цепочка INPUT целиком: по её порядку видно, работают ли правила ufw
	// вообще (на oracle-3 стоковое правило Oracle стоит выше — DESIGN.md §2.6).
	Input []string `json:"input"`
	// UFW — правила из /etc/ufw/user.rules, прочитанные как файл: запускать сам
	// ufw не нужно, а значит и его прав тоже.
	UFW []string `json:"ufw"`
	// UFWWorks — правила ufw реально применяются к хостовым портам?
	UFWWorks bool   `json:"ufw_works"`
	Error    string `json:"error,omitempty"`
}

func Run(ctx context.Context, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.OutFile), 0o755); err != nil {
		return err
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	write(cfg)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			write(cfg)
		}
	}
}

func write(cfg Config) {
	d := collect(cfg)
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
	}
	tmp := cfg.OutFile + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, cfg.OutFile)
	}
}

func collect(cfg Config) Dump {
	d := Dump{Time: time.Now()}

	if out, err := run("iptables", "-S", "DOCKER-USER"); err == nil {
		d.DockerUser = lines(out)
	} else {
		d.Error = "DOCKER-USER: " + err.Error()
	}
	if out, err := run("iptables", "-S", "INPUT"); err == nil {
		d.Input = lines(out)
		d.UFWWorks = ufwReachable(d.Input)
	} else if d.Error == "" {
		d.Error = "INPUT: " + err.Error()
	}
	if b, err := os.ReadFile(cfg.UFWRules); err == nil {
		for _, l := range lines(string(b)) {
			if strings.HasPrefix(l, "-A ufw-user-input") {
				d.UFW = append(d.UFW, l)
			}
		}
	}
	return d
}

// ufwReachable проверяет главную неочевидность: доходит ли трафик до цепочек ufw.
// Если стоковое правило Oracle «REJECT» стоит раньше перехода в ufw-before-input,
// то любые правила ufw, кроме уже разрешённых выше, — мёртвый код.
func ufwReachable(input []string) bool {
	for _, l := range input {
		if strings.Contains(l, "ufw-before-input") {
			return true
		}
		if strings.Contains(l, "-j REJECT") || strings.Contains(l, "-j DROP") {
			if !strings.Contains(l, "--dport") && !strings.Contains(l, "-s ") && !strings.Contains(l, "-i ") {
				return false // безусловный REJECT/DROP раньше ufw
			}
		}
	}
	return false
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

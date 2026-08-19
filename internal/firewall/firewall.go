// Package firewall — вердикт «видно ли порт из интернета».
//
// Три слоя, которые надо не путать (DESIGN.md §2.16 и §2.6):
//
//	порт опубликован докером  → фильтруется ТОЛЬКО цепочкой DOCKER-USER,
//	                            ufw его не видит вовсе (DNAT идёт в FORWARD);
//	порт держит процесс хоста → фильтруется ufw, но лишь если до цепочек ufw
//	                            вообще доходит трафик;
//	Security List в Oracle     → панель туда не ходит и об этом честно говорит.
package firewall

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vladbogun1/edgedeck/internal/fwdump"
)

type Reader struct {
	file string

	mu     sync.Mutex
	dump   fwdump.Dump
	loaded time.Time
}

func New(file string) *Reader { return &Reader{file: file} }

// Dump возвращает последний снимок правил. Второе значение — свежий ли он:
// устаревшему верить нельзя, и панель об этом скажет.
func (r *Reader) Dump() (fwdump.Dump, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.loaded) > 15*time.Second {
		if b, err := os.ReadFile(r.file); err == nil {
			var d fwdump.Dump
			if json.Unmarshal(b, &d) == nil {
				r.dump = d
			}
		}
		r.loaded = time.Now()
	}
	fresh := !r.dump.Time.IsZero() && time.Since(r.dump.Time) < 5*time.Minute
	return r.dump, fresh
}

// Verdict — что панель говорит про конкретный порт.
type Verdict struct {
	Level   string   `json:"level"`   // open | limited | closed | unknown
	Summary string   `json:"summary"` // одной строкой для списка
	Detail  string   `json:"detail"`  // объяснение, откуда вывод
	Advice  string   `json:"advice"`  // готовая команда или шаг
	Allowed []string `json:"allowed"` // кому разрешено, если ограничено
}

var (
	dportRe = regexp.MustCompile(`--dport\s+(\d+)`)
	srcRe   = regexp.MustCompile(`-s\s+(\S+)`)
)

// Judge выносит вердикт для порта.
//
// fromDocker — порт опубликован докером (его держит docker-proxy);
// loopbackOnly — слушает только 127.0.0.1.
func (r *Reader) Judge(port int, proto string, fromDocker, loopbackOnly bool) Verdict {
	if loopbackOnly {
		return Verdict{
			Level:   "closed",
			Summary: "только с самого сервера",
			Detail:  "порт слушает 127.0.0.1: из интернета не видно, но и из другого контейнера тоже — им нужна docker-сеть",
		}
	}

	dump, fresh := r.Dump()
	if !fresh {
		return Verdict{
			Level:   "unknown",
			Summary: "правила фаервола не прочитаны",
			Detail:  "нет свежего снимка правил: контейнер edgedeck-firewall не запущен или не смог прочитать iptables",
			Advice:  "cd ~/edgedeck && docker compose up -d firewall",
		}
	}

	if fromDocker {
		allowed, hasDrop := dockerUserRules(dump.DockerUser, port)
		switch {
		case len(allowed) > 0 && hasDrop:
			return Verdict{
				Level:   "limited",
				Summary: "только с " + strings.Join(allowed, ", "),
				Detail:  "правило в цепочке DOCKER-USER пускает эти адреса, остальное отбрасывается",
				Allowed: allowed,
			}
		case hasDrop:
			return Verdict{
				Level:   "closed",
				Summary: "закрыт в DOCKER-USER",
				Detail:  "цепочка DOCKER-USER отбрасывает трафик на этот порт",
			}
		default:
			return Verdict{
				Level:   "open",
				Summary: "видно из интернета",
				Detail: "порт опубликован докером, а в цепочке DOCKER-USER для него правил нет. " +
					"ufw тут не помогает: такой трафик идёт через DNAT в FORWARD, минуя INPUT",
				Advice: fmt.Sprintf("echo %q >> /opt/docker-acl/ports.conf && sudo /opt/docker-acl/apply.sh", fmt.Sprintf("%d\t<кому можно>/32", port)),
			}
		}
	}

	// Хостовой порт: решают правила ufw, и только если до них доходит трафик.
	if !dump.UFWWorks {
		return Verdict{
			Level:   "closed",
			Summary: "недостижим (правила ufw не работают)",
			Detail: "в цепочке INPUT безусловный REJECT стоит раньше перехода в ufw, поэтому хостовые порты " +
				"недоступны ни снаружи, ни из контейнеров — и добавление правил ufw это не меняет",
			Advice: "нужен порядок: sudo iptables -I INPUT -p " + proto + " --dport " + strconv.Itoa(port) + " -j ACCEPT",
		}
	}
	if allowed, ok := ufwAllows(dump.UFW, port); ok {
		if len(allowed) > 0 {
			return Verdict{
				Level:   "limited",
				Summary: "только с " + strings.Join(allowed, ", "),
				Detail:  "правило ufw разрешает эти адреса",
				Allowed: allowed,
			}
		}
		return Verdict{
			Level:   "open",
			Summary: "видно из интернета",
			Detail:  "правило ufw разрешает этот порт всем",
			Advice:  "sudo ufw delete allow " + strconv.Itoa(port) + " — если доступ снаружи не нужен",
		}
	}
	return Verdict{
		Level:   "closed",
		Summary: "закрыт (нет правила ufw)",
		Detail:  "порт слушает, но ufw его не разрешает, а политика по умолчанию — запрет",
	}
}

// dockerUserRules разбирает правила цепочки DOCKER-USER для порта.
func dockerUserRules(rules []string, port int) (allowed []string, hasDrop bool) {
	for _, rule := range rules {
		m := dportRe.FindStringSubmatch(rule)
		if m == nil {
			continue
		}
		if p, _ := strconv.Atoi(m[1]); p != port {
			continue
		}
		switch {
		case strings.Contains(rule, "-j DROP"), strings.Contains(rule, "-j REJECT"):
			hasDrop = true
		case strings.Contains(rule, "-j RETURN"), strings.Contains(rule, "-j ACCEPT"):
			if src := srcRe.FindStringSubmatch(rule); src != nil {
				allowed = append(allowed, src[1])
			}
		}
	}
	return allowed, hasDrop
}

// ufwAllows ищет правило ufw для порта. Второе значение — нашлось ли вообще.
func ufwAllows(rules []string, port int) ([]string, bool) {
	var allowed []string
	found := false
	for _, rule := range rules {
		m := dportRe.FindStringSubmatch(rule)
		if m == nil {
			continue
		}
		if p, _ := strconv.Atoi(m[1]); p != port {
			continue
		}
		if !strings.Contains(rule, "-j ACCEPT") {
			continue
		}
		found = true
		if src := srcRe.FindStringSubmatch(rule); src != nil && src[1] != "0.0.0.0/0" {
			allowed = append(allowed, src[1])
		}
	}
	return allowed, found
}

// Health — состояние самого сборщика правил, для экрана настроек.
func (r *Reader) Health() (ok bool, message string) {
	dump, fresh := r.Dump()
	switch {
	case dump.Time.IsZero():
		return false, "снимок правил ни разу не появился: контейнер edgedeck-firewall не запущен"
	case !fresh:
		return false, "снимок правил устарел (" + dump.Time.Format("15:04:05") + ") — сборщик, похоже, встал"
	case dump.Error != "":
		return false, "сборщик работает, но с ошибкой: " + dump.Error
	default:
		msg := "правила прочитаны " + dump.Time.Format("15:04:05")
		if !dump.UFWWorks {
			msg += "; внимание: правила ufw на этом сервере не применяются — REJECT стоит выше них"
		}
		return true, msg
	}
}

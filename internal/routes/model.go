// Package routes — маршруты как данные.
//
// Источник истины — файл ~/edge/routes.yaml: человекочитаемый, его можно править
// руками по SSH, если панель мертва (DESIGN.md §5.1). Файлы sites/*.caddy из него
// генерируются и правиться руками не должны — на это есть шапка-предупреждение.
//
// Файлы, которых нет в routes.yaml, панель не трогает вообще: сайты, заведённые
// через bin/newsite, продолжают работать и показываются как «руками».
package routes

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Тип маршрута. Пять штук, каждый из реального случая (DESIGN.md §3).
const (
	TypeProxy    = "proxy"     // проксировать на цель
	TypeStatic   = "static"    // отдавать папку
	TypeRedirect = "redirect"  // 301 на другой адрес
	TypeACMEOnly = "acme_only" // только продление сертификата, остальное 404
	TypeStub     = "stub"      // заглушка «скоро откроется»
)

// Вид цели.
const (
	KindContainer = "container" // имя контейнера в сети edge
	KindHost      = "host"      // процесс на хосте (host.docker.internal)
	KindExternal  = "external"  // другой сервер по URL
)

type Config struct {
	Version int     `yaml:"version" json:"version"`
	Routes  []Route `yaml:"routes" json:"routes"`
}

type Route struct {
	// Domain — основное имя. ID маршрута = domain + path.
	Domain  string   `yaml:"domain" json:"domain"`
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`

	// Path — «/» для всего сайта или «/logs/» для части (DESIGN.md §2.10).
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	Type   string  `yaml:"type" json:"type"`
	Target *Target `yaml:"target,omitempty" json:"target,omitempty"`

	// RedirectTo — куда, если type: redirect.
	RedirectTo string `yaml:"redirect_to,omitempty" json:"redirect_to,omitempty"`

	// MaxUpload — «максимальный размер загрузки», например 128MB.
	// Без него Vaultwarden отдавал 413 на вложениях (DESIGN.md §2.13).
	MaxUpload string `yaml:"max_upload,omitempty" json:"max_upload,omitempty"`

	// ReadTimeout — сколько ждать ответа. Пусто = не ограничивать: в Caddy
	// таймаута по умолчанию нет, и живые логи с WebSocket не рвутся сами.
	ReadTimeout string `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty"`

	SecurityHeaders bool `yaml:"security_headers" json:"security_headers"`

	// HSTS — билет в одну сторону: браузер запомнит на год (DESIGN.md §2.14).
	HSTS bool `yaml:"hsts" json:"hsts"`

	// HideUpstreamCSP — снять CSP апстрима, чтобы не было двух заголовков
	// (DESIGN.md §2.11). Ставится только вместе со своим CSP.
	HideUpstreamCSP bool   `yaml:"hide_upstream_csp,omitempty" json:"hide_upstream_csp,omitempty"`
	CSP             string `yaml:"csp,omitempty" json:"csp,omitempty"`

	// Auth — «требовать вход»: спросить у чужой панели, есть ли сессия.
	Auth *ForwardAuth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// AllowIPs — пускать только с этих адресов.
	AllowIPs []string `yaml:"allow_ips,omitempty" json:"allow_ips,omitempty"`

	// DenyPaths — пути, которые наружу не отдаём (404, а не 403: не подтверждаем
	// существование). Осторожно: правило бьёт и по браузеру (DESIGN.md §2.12).
	DenyPaths []string `yaml:"deny_paths,omitempty" json:"deny_paths,omitempty"`

	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`
}

type Target struct {
	Kind string `yaml:"kind" json:"kind"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"` // контейнер или хостовое имя
	Port int    `yaml:"port,omitempty" json:"port,omitempty"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"` // для external
}

type ForwardAuth struct {
	Upstream string `yaml:"upstream" json:"upstream"` // контейнер:порт панели-авторизатора
	URI      string `yaml:"uri" json:"uri"`           // путь проверки, например /api/authcheck
}

// ID — устойчивый идентификатор маршрута для интерфейса.
func (r Route) ID() string {
	p := r.Path
	if p == "" {
		p = "/"
	}
	return r.Domain + p
}

// Addr — как цель выглядит в конфиге Caddy.
func (t *Target) Addr() string {
	if t == nil {
		return ""
	}
	switch t.Kind {
	case KindExternal:
		return t.URL
	case KindHost:
		// Процесс вне докера. На oracle-3 это ещё требует правки порядка правил
		// iptables — см. DESIGN.md §2.6, панель об этом предупреждает.
		return fmt.Sprintf("host.docker.internal:%d", t.Port)
	default:
		return fmt.Sprintf("%s:%d", t.Name, t.Port)
	}
}

// ---- файл ----

func Path(edgeDir string) string { return filepath.Join(edgeDir, "routes.yaml") }

func Load(edgeDir string) (Config, error) {
	b, err := os.ReadFile(Path(edgeDir))
	if os.IsNotExist(err) {
		return Config{Version: 1}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("routes.yaml не разобрался: %w", err)
	}
	if c.Version == 0 {
		c.Version = 1
	}
	for i := range c.Routes {
		if c.Routes[i].Path == "" {
			c.Routes[i].Path = "/"
		}
	}
	return c, nil
}

const fileHeader = `# Маршруты EdgeDeck — источник истины.
#
# Этот файл можно править руками: панель его читает, а не перетирает молча.
# Из него генерируются sites/*.caddy — вот их править не надо, они перезапишутся.
#
# После правки руками: примените изменения в панели или выполните
#   docker exec edge_caddy caddy reload --config /etc/caddy/Caddyfile
`

// Save пишет routes.yaml. Владелец файла выставляется как у каталога:
// панель работает от root, а по SSH сюда ходит ubuntu, и файлы должны
// оставаться доступными ему на запись (иначе «правится руками» — ложь).
func Save(edgeDir string, c Config) error {
	c.Version = 1
	sort.SliceStable(c.Routes, func(i, j int) bool {
		if c.Routes[i].Domain != c.Routes[j].Domain {
			return c.Routes[i].Domain < c.Routes[j].Domain
		}
		return c.Routes[i].Path < c.Routes[j].Path
	})
	body, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return WriteFileAsDirOwner(Path(edgeDir), append([]byte(fileHeader), body...))
}

// ---- проверки формы ----

var domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// Validate проверяет то, что можно проверить без сети, и объясняет по-русски.
func (r *Route) Validate() error {
	r.Domain = strings.ToLower(strings.TrimSpace(r.Domain))
	if !domainRe.MatchString(r.Domain) {
		return fmt.Errorf("«%s» не похоже на домен", r.Domain)
	}
	if net.ParseIP(r.Domain) != nil {
		return fmt.Errorf("это IP-адрес, а не домен: маршруты делаются на домены")
	}
	if r.Path == "" {
		r.Path = "/"
	}
	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("путь должен начинаться со «/», а не «%s»", r.Path)
	}
	for i, a := range r.Aliases {
		a = strings.ToLower(strings.TrimSpace(a))
		if !domainRe.MatchString(a) {
			return fmt.Errorf("«%s» в списке дополнительных имён не похоже на домен", a)
		}
		r.Aliases[i] = a
	}

	switch r.Type {
	case TypeProxy:
		if r.Target == nil {
			return fmt.Errorf("не указана цель")
		}
		switch r.Target.Kind {
		case KindContainer, KindHost:
			if r.Target.Name == "" && r.Target.Kind == KindContainer {
				return fmt.Errorf("не указано имя контейнера")
			}
			if r.Target.Port <= 0 || r.Target.Port > 65535 {
				return fmt.Errorf("порт %d вне диапазона", r.Target.Port)
			}
		case KindExternal:
			if !strings.HasPrefix(r.Target.URL, "http://") && !strings.HasPrefix(r.Target.URL, "https://") {
				return fmt.Errorf("внешний адрес должен начинаться с http:// или https://")
			}
		default:
			return fmt.Errorf("непонятный вид цели «%s»", r.Target.Kind)
		}
	case TypeRedirect:
		if r.RedirectTo == "" {
			return fmt.Errorf("не указано, куда перенаправлять")
		}
	case TypeStatic, TypeACMEOnly, TypeStub:
		// целей не требуют
	default:
		return fmt.Errorf("непонятный тип маршрута «%s»", r.Type)
	}

	if r.MaxUpload != "" && !sizeRe.MatchString(r.MaxUpload) {
		return fmt.Errorf("размер загрузки «%s» непонятен, ожидается вид 128MB", r.MaxUpload)
	}
	if r.ReadTimeout != "" && !durationRe.MatchString(r.ReadTimeout) {
		return fmt.Errorf("таймаут «%s» непонятен, ожидается вид 30s или 1h", r.ReadTimeout)
	}
	for _, ip := range r.AllowIPs {
		if net.ParseIP(ip) == nil {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				return fmt.Errorf("«%s» не адрес и не подсеть", ip)
			}
		}
	}
	if r.HideUpstreamCSP && r.CSP == "" {
		return fmt.Errorf("снимать CSP апстрима без своей политики опасно: страница останется без CSP вообще")
	}
	return nil
}

var (
	sizeRe     = regexp.MustCompile(`^\d+(B|KB|MB|GB|KiB|MiB|GiB)?$`)
	durationRe = regexp.MustCompile(`^\d+(ns|us|ms|s|m|h)$`)
)

// Conflict ищет столкновение с уже существующими маршрутами.
// Один и тот же домен+путь дважды — самая обидная ошибка: второй файл просто
// перебивал бы первый, и понять это по конфигу было бы нельзя.
func Conflict(existing []Route, candidate Route, ignoreID string) error {
	for _, e := range existing {
		if e.ID() == ignoreID {
			continue
		}
		if e.ID() == candidate.ID() {
			return fmt.Errorf("маршрут %s уже есть", candidate.ID())
		}
		// Домен не может быть одновременно основным у одного маршрута и
		// дополнительным именем у другого — Caddy не поймёт, кому он принадлежит.
		for _, a := range e.Aliases {
			if a == candidate.Domain {
				return fmt.Errorf("%s уже указан как дополнительное имя у %s", candidate.Domain, e.ID())
			}
		}
		for _, a := range candidate.Aliases {
			if a == e.Domain {
				return fmt.Errorf("%s — это основной домен маршрута %s", a, e.ID())
			}
		}
	}
	return nil
}

// FindByID ищет маршрут и его индекс.
func FindByID(list []Route, id string) (int, bool) {
	for i, r := range list {
		if r.ID() == id {
			return i, true
		}
	}
	return -1, false
}

// ---- запись файлов с сохранением владельца ----

// WriteFileAsDirOwner пишет файл и выставляет ему владельца каталога.
//
// Панель в контейнере работает от root, а каталог ~/edge принадлежит ubuntu.
// Без этого сгенерированные файлы стали бы root-only, и обещание «всё можно
// поправить руками по SSH» превратилось бы в неправду (DESIGN.md §2.19).
func WriteFileAsDirOwner(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	if uid, gid, ok := ownerOf(dir); ok {
		_ = os.Chown(tmp, uid, gid)
	}
	return os.Rename(tmp, path)
}

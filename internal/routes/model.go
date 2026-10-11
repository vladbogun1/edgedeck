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

	// SSO — пускать только после единого входа EdgeDeck (код в Telegram):
	// forward_auth к локальной панели, без входа — редирект на вход и обратно.
	// Для своих сервисов (JARVIS, логи, Portainer, PufferPanel); не для того,
	// у чего свои клиенты (Vaultwarden).
	SSO bool `yaml:"sso,omitempty" json:"sso,omitempty"`

	// Auth — «требовать вход»: спросить у чужой панели, есть ли сессия.
	Auth *ForwardAuth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// AllowIPs — пускать только с этих адресов.
	AllowIPs []string `yaml:"allow_ips,omitempty" json:"allow_ips,omitempty"`

	// DenyPaths — пути, которые наружу не отдаём (404, а не 403: не подтверждаем
	// существование). Осторожно: правило бьёт и по браузеру (DESIGN.md §2.12).
	DenyPaths []string `yaml:"deny_paths,omitempty" json:"deny_paths,omitempty"`

	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`

	// ---- то, без чего сайты оставались «руками» (октябрь 2026) ----

	// Headers — свои заголовки ответа в точном виде, например HSTS на 2 года с
	// includeSubDomains или Referrer-Policy строже, чем в common. Порядок
	// сохраняется. Значение «-» удаляет заголовок. Относятся к сайту целиком.
	Headers []Header `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Compress — сжатие ответа (encode zstd gzip) без остального набора common:
	// Mini App в iframe Telegram ломается от X-Frame-Options, а сжатие нужно.
	Compress bool `yaml:"compress,omitempty" json:"compress,omitempty"`

	// PathRedirects — точечные редиректы внутри сайта, например /logs → /logs/.
	PathRedirects []PathRedirect `yaml:"path_redirects,omitempty" json:"path_redirects,omitempty"`

	// ACMEWebroot — на :80 отдавать проверки чужого certbot из этой папки
	// (в контейнере Caddy). Нужно, когда сертификат на это же имя выпускает
	// кто-то ещё — например, Caddy магазина для портов 666/667. Остальной http
	// уходит туда же, куда и https.
	ACMEWebroot string `yaml:"acme_webroot,omitempty" json:"acme_webroot,omitempty"`

	// Flush — отдавать ответ сразу, без буфера (nginx proxy_buffering off):
	// события, стримы, камера.
	Flush bool `yaml:"flush,omitempty" json:"flush,omitempty"`

	// DialTimeout — сколько ждать соединения с целью, например 5s.
	DialTimeout string `yaml:"dial_timeout,omitempty" json:"dial_timeout,omitempty"`
}

type Header struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

type PathRedirect struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Code int    `yaml:"code,omitempty" json:"code,omitempty"` // 301 по умолчанию
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
	// CopyHeaders — какие заголовки ответа авторизатора передать дальше в цель.
	CopyHeaders []string `yaml:"copy_headers,omitempty" json:"copy_headers,omitempty"`
	// HeaderUp — что добавить в запрос к авторизатору, например
	// X-Original-URI: {uri} — как nginx auth_request.
	HeaderUp []Header `yaml:"header_up,omitempty" json:"header_up,omitempty"`
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

var (
	headerNameRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	safePathRe   = regexp.MustCompile(`^/[^\s{}"]*$`)
)

func hasHeader(list []Header, name string) bool {
	for _, h := range list {
		if strings.EqualFold(h.Name, name) && h.Value != "-" {
			return true
		}
	}
	return false
}

func checkHeader(h Header) error {
	if !headerNameRe.MatchString(h.Name) {
		return fmt.Errorf("«%s» не похоже на имя заголовка", h.Name)
	}
	if strings.ContainsAny(h.Value, "\r\n") {
		return fmt.Errorf("в значении заголовка %s перевод строки", h.Name)
	}
	return nil
}

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
	if r.DialTimeout != "" && !durationRe.MatchString(r.DialTimeout) {
		return fmt.Errorf("таймаут соединения «%s» непонятен, ожидается вид 5s", r.DialTimeout)
	}
	// Значения попадают в Caddyfile: перевод строки или фигурная скобка в них
	// позволили бы дописать в конфиг свою директиву.
	for _, h := range r.Headers {
		if err := checkHeader(h); err != nil {
			return err
		}
	}
	if r.Auth != nil {
		for _, h := range r.Auth.HeaderUp {
			if err := checkHeader(h); err != nil {
				return err
			}
		}
		for _, n := range r.Auth.CopyHeaders {
			if !headerNameRe.MatchString(n) {
				return fmt.Errorf("«%s» не похоже на имя заголовка", n)
			}
		}
	}
	for _, pr := range r.PathRedirects {
		if !safePathRe.MatchString(pr.From) || !safePathRe.MatchString(pr.To) {
			return fmt.Errorf("редирект «%s → %s»: пути должны начинаться со «/» и быть без пробелов", pr.From, pr.To)
		}
		if pr.Code != 0 && (pr.Code < 300 || pr.Code > 308) {
			return fmt.Errorf("код редиректа %d — ожидается 301, 302, 307 или 308", pr.Code)
		}
	}
	if r.ACMEWebroot != "" && !safePathRe.MatchString(r.ACMEWebroot) {
		return fmt.Errorf("папка проверок «%s» должна быть абсолютным путём без пробелов", r.ACMEWebroot)
	}
	for _, ip := range r.AllowIPs {
		if net.ParseIP(ip) == nil {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				return fmt.Errorf("«%s» не адрес и не подсеть", ip)
			}
		}
	}
	if r.SSO && r.Auth != nil && r.Auth.Upstream != "" {
		return fmt.Errorf("единый вход и вход через другую панель одновременно — выберите одно")
	}
	if r.HideUpstreamCSP && r.CSP == "" && !hasHeader(r.Headers, "Content-Security-Policy") {
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

	// Владельца берём у ближайшего СУЩЕСТВУЮЩЕГО каталога выше: если каталог
	// создаём мы сами, его владельцем стал бы root, и файл унаследовал бы root —
	// то есть по SSH его было бы не поправить.
	uid, gid, haveOwner := ownerOf(nearestExisting(dir))
	missing := missingDirs(dir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if haveOwner {
		// Отдаём владельцу ТОЛЬКО те каталоги, которых до нас не было.
		for _, d := range missing {
			_ = os.Chown(d, uid, gid)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	if haveOwner {
		_ = os.Chown(tmp, uid, gid)
	}
	return os.Rename(tmp, path)
}

// nearestExisting поднимается вверх по пути до первого каталога, который есть.
func nearestExisting(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// missingDirs — каталоги, которых сейчас нет и которые создаст MkdirAll.
// Считается ДО создания: только их потом и можно отдавать другому владельцу,
// трогать чужие существующие каталоги нельзя.
func missingDirs(dir string) []string {
	var out []string
	for {
		if _, err := os.Stat(dir); err == nil {
			return out
		}
		out = append(out, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
		dir = parent
	}
}

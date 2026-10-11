package routes

import (
	"fmt"
	"sort"
	"strings"
)

// GeneratedMarker — по этой строке панель узнаёт свои файлы. Всё, что её не
// содержит, считается написанным руками и не перезаписывается никогда.
const GeneratedMarker = "СГЕНЕРИРОВАНО EdgeDeck"

// Где лежит страница ошибки. Каталог внутри www, имя с подчёркиванием — чтобы
// не столкнуться с каталогом настоящего домена.
const (
	ErrorPageDir  = "_edgedeck"
	ErrorPageFile = "error.html"
)

// PanelUpstream — адрес панели EdgeDeck в сети Caddy.
const PanelUpstream = "edgedeck:4000"

// SSOCookieStrip — регулярка, которой из запросов к приложениям вырезается
// cookie единого входа (edgedeck_sso): токен пускает во все свои сервисы и
// панели, и приложение на поддомене не должно его видеть.
const SSOCookieStrip = `edgedeck_sso=[^;]*;? *`

// HideStack срезает с ответа то, что выдаёт начинку сайта: версию nginx за
// прокси, «X-Powered-By: Next.js», «Via: 1.1 Caddy». Защиты это не даёт —
// сканеры узнают софт и по поведению, — но и раздавать версии незачем.
// Удаление в Caddy отложенное, поэтому срабатывает и на заголовки, пришедшие
// от цели reverse_proxy. Ставится в каждый сайт, независимо от import common:
// common подключён не везде.
const HideStack = "\theader {\n\t\t-Server\n\t\t-X-Powered-By\n\t\t-Via\n\t}\n"

// Render собирает файлы sites/*.caddy из маршрутов: имя файла → содержимое.
//
// Один домен = один файл, даже если у него несколько маршрутов по путям:
// так правка одного сайта физически не может сломать другой (DESIGN.md §2.1).
func Render(list []Route) map[string]string {
	byDomain := map[string][]Route{}
	for _, r := range list {
		byDomain[r.Domain] = append(byDomain[r.Domain], r)
	}
	out := map[string]string{}
	for domain, routes := range byDomain {
		// Более длинный путь — более конкретный, идёт первым. Caddy и сам
		// сортирует matchers, но в файле человеку так понятнее.
		sort.SliceStable(routes, func(i, j int) bool { return len(routes[i].Path) > len(routes[j].Path) })
		out[domain+".caddy"] = renderDomain(domain, routes)
	}
	return out
}

func renderDomain(domain string, list []Route) string {
	var b strings.Builder
	addresses := []string{domain}
	seen := map[string]bool{domain: true}
	for _, r := range list {
		for _, a := range r.Aliases {
			if !seen[a] {
				seen[a] = true
				addresses = append(addresses, a)
			}
		}
	}

	fmt.Fprintf(&b, "# %s — %s\n", GeneratedMarker, domain)
	fmt.Fprintf(&b, "# Файл собран из routes.yaml. Правки здесь потеряются при следующем\n")
	fmt.Fprintf(&b, "# применении — правьте routes.yaml или маршрут в панели.\n")
	for _, r := range list {
		if r.Comment != "" {
			fmt.Fprintf(&b, "#\n# %s: %s\n", r.Path, r.Comment)
		}
	}
	// Общие для домена вещи берём у корневого маршрута («/»), а если его нет — у
	// первого: заголовки и HSTS относятся к сайту целиком, а не к отдельному пути.
	head := list[0]
	for _, r := range list {
		if r.Path == "/" {
			head = r
			break
		}
	}

	if head.ACMEWebroot != "" {
		b.WriteString(renderACMEBlock(domain, addresses, head))
	}

	fmt.Fprintf(&b, "\n%s {\n", strings.Join(addresses, ", "))
	b.WriteString(HideStack)

	if head.Compress {
		b.WriteString("\tencode zstd gzip\n")
	}
	if len(head.Headers) > 0 {
		b.WriteString("\theader {\n")
		for _, h := range head.Headers {
			b.WriteString("\t\t" + headerLine(h) + "\n")
		}
		b.WriteString("\t}\n")
	}
	if len(head.PathRedirects) > 0 {
		b.WriteString("\n")
		for _, pr := range head.PathRedirects {
			code := pr.Code
			if code == 0 {
				code = 301
			}
			fmt.Fprintf(&b, "\tredir %s %s %d\n", pr.From, pr.To, code)
		}
	}
	if head.SecurityHeaders {
		b.WriteString("\timport common\n")
	}
	if head.HSTS {
		b.WriteString("\timport hsts\n")
	}
	if len(head.AllowIPs) > 0 {
		fmt.Fprintf(&b, "\n\t# Пускать только со своих адресов\n")
		fmt.Fprintf(&b, "\t@not_allowed not remote_ip %s\n", strings.Join(head.AllowIPs, " "))
		b.WriteString("\trespond @not_allowed \"нельзя\" 403\n")
	}
	if head.MaxUpload != "" {
		fmt.Fprintf(&b, "\n\trequest_body {\n\t\tmax_size %s\n\t}\n", head.MaxUpload)
	}
	if len(head.DenyPaths) > 0 {
		fmt.Fprintf(&b, "\n\t# Закрытые пути: 404, а не 403 — не подтверждаем, что эндпоинт есть\n")
		for _, p := range head.DenyPaths {
			fmt.Fprintf(&b, "\thandle %s {\n\t\trespond \"\" 404\n\t}\n", denyPattern(p))
		}
	}

	// Если у сайта есть другие handle (пути или закрытые пути), корень тоже
	// уходит в свой handle — как в файлах, написанных руками: так обработчики
	// взаимоисключающие и порядок директив Caddy ни на что не влияет.
	wrapRoot := len(list) > 1 || len(head.DenyPaths) > 0
	for _, r := range list {
		b.WriteString("\n")
		switch {
		case r.Path == "/" && !wrapRoot:
			b.WriteString(indent(renderBody(r, domain), "\t"))
		case r.Path == "/":
			b.WriteString("\thandle {\n")
			b.WriteString(indent(renderBody(r, domain), "\t\t"))
			b.WriteString("\t}\n")
		default:
			fmt.Fprintf(&b, "\thandle %s {\n", pathPattern(r.Path))
			// Размер загрузки у отдельного пути — свой (Vaultwarden на /vault/),
			// у корня он уже выставлен на весь сайт выше.
			if r.MaxUpload != "" {
				fmt.Fprintf(&b, "\t\trequest_body {\n\t\t\tmax_size %s\n\t\t}\n", r.MaxUpload)
			}
			b.WriteString(indent(renderBody(r, domain), "\t\t"))
			b.WriteString("\t}\n")
		}
	}

	// Страница вместо голого «502 Bad Gateway». Перезапуск контейнера — это
	// обычная жизнь, и посетитель в такой момент должен видеть внятное «сервис
	// вернётся через несколько секунд», а не то, что сайт сломался.
	//
	// status сохраняет настоящий код ответа: поисковики и мониторинги обязаны
	// видеть 502, а не 200 с текстом об ошибке.
	fmt.Fprintf(&b, "\n\thandle_errors {\n")
	fmt.Fprintf(&b, "\t\troot * /var/www/%s\n", ErrorPageDir)
	fmt.Fprintf(&b, "\t\trewrite * /%s\n", ErrorPageFile)
	fmt.Fprintf(&b, "\t\ttemplates\n")
	fmt.Fprintf(&b, "\t\tfile_server {\n\t\t\tstatus {err.status_code}\n\t\t}\n")
	fmt.Fprintf(&b, "\t}\n")

	fmt.Fprintf(&b, "\n\tlog {\n\t\toutput file /var/log/caddy/%s.log {\n\t\t\troll_size 10MiB\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", domain)
	b.WriteString("}\n")
	return b.String()
}

func renderBody(r Route, domain string) string {
	var b strings.Builder

	if r.SSO {
		b.WriteString("# Единый вход EdgeDeck: без входа — на страницу входа и обратно\n")
		fmt.Fprintf(&b, "forward_auth %s {\n\turi /internal/sso/check\n}\n", PanelUpstream)
	}
	if r.Auth != nil && r.Auth.Upstream != "" {
		fmt.Fprintf(&b, "# Требовать вход: спрашиваем у %s, есть ли активная сессия\n", r.Auth.Upstream)
		fmt.Fprintf(&b, "forward_auth %s {\n\turi %s\n", r.Auth.Upstream, r.Auth.URI)
		for _, h := range r.Auth.HeaderUp {
			fmt.Fprintf(&b, "\theader_up %s %s\n", h.Name, quoteValue(h.Value))
		}
		if len(r.Auth.CopyHeaders) > 0 {
			fmt.Fprintf(&b, "\tcopy_headers %s\n", strings.Join(r.Auth.CopyHeaders, " "))
		}
		b.WriteString("}\n")
	}

	switch r.Type {
	case TypeProxy:
		fmt.Fprintf(&b, "reverse_proxy %s {\n", r.Target.Addr())
		b.WriteString("\theader_up X-Real-IP {remote_host}\n")
		if r.Target.Addr() != PanelUpstream {
			// Токен единого входа нужен только панели: в приложения его не отдаём.
			fmt.Fprintf(&b, "\theader_up Cookie %s \"\"\n", quoteValue(SSOCookieStrip))
		}
		if r.HideUpstreamCSP {
			b.WriteString("\t# Снимаем CSP апстрима: два заголовка браузер объединит по пересечению\n")
			b.WriteString("\t# и страница сломается на первом же расхождении (DESIGN.md §2.11)\n")
			b.WriteString("\theader_down -Content-Security-Policy\n")
		}
		if r.Flush {
			b.WriteString("\t# Без буфера: события и стримы уходят клиенту сразу\n")
			b.WriteString("\tflush_interval -1\n")
		}
		if r.ReadTimeout != "" || r.DialTimeout != "" {
			b.WriteString("\ttransport http {\n")
			if r.DialTimeout != "" {
				fmt.Fprintf(&b, "\t\tdial_timeout %s\n", r.DialTimeout)
			}
			if r.ReadTimeout != "" {
				fmt.Fprintf(&b, "\t\tread_timeout %s\n", r.ReadTimeout)
			}
			b.WriteString("\t}\n")
		}
		b.WriteString("}\n")
		if r.CSP != "" {
			fmt.Fprintf(&b, "header Content-Security-Policy %q\n", r.CSP)
		}

	case TypeStatic:
		fmt.Fprintf(&b, "root * /var/www/%s\n", domain)
		b.WriteString("file_server\n")
		b.WriteString("try_files {path} /index.html\n")

	case TypeRedirect:
		fmt.Fprintf(&b, "redir %s{uri} permanent\n", strings.TrimRight(r.RedirectTo, "/"))

	case TypeACMEOnly:
		b.WriteString("# Только продление сертификата: проверки ACME Caddy обрабатывает сам,\n")
		b.WriteString("# всё остальное закрыто. Так живут домены, чей TLS терминируется в другом месте.\n")
		b.WriteString("respond \"\" 404\n")

	case TypeStub:
		b.WriteString("# Заглушка: домен занят, сайта пока нет.\n")
		fmt.Fprintf(&b, "respond \"%s — скоро тут будет сайт\" 200\n", domain)
	}

	return b.String()
}

// renderACMEBlock — отдельный блок :80 для сайта, чей сертификат выпускает ещё
// и чужой certbot (HTTP-01 webroot). Свои проверки Caddy перехватывает раньше
// маршрутов и только по своим токенам, чужие проваливаются в webroot — друг
// другу они не мешают. Явный http-блок отключает авторедирект Caddy для этих
// имён, поэтому всё остальное уходит туда же, куда и https.
func renderACMEBlock(domain string, addresses []string, head Route) string {
	var b strings.Builder
	http := make([]string, len(addresses))
	for i, a := range addresses {
		http[i] = "http://" + a
	}
	fmt.Fprintf(&b, "\n%s {\n", strings.Join(http, ", "))
	b.WriteString(HideStack)
	fmt.Fprintf(&b, "\thandle /.well-known/acme-challenge/* {\n\t\troot * %s\n\t\tfile_server\n\t}\n", head.ACMEWebroot)
	b.WriteString("\thandle {\n")
	if head.Type == TypeRedirect {
		fmt.Fprintf(&b, "\t\tredir %s{uri} permanent\n", strings.TrimRight(head.RedirectTo, "/"))
	} else {
		b.WriteString("\t\tredir https://{host}{uri} permanent\n")
	}
	b.WriteString("\t}\n")
	fmt.Fprintf(&b, "\n\tlog {\n\t\toutput file /var/log/caddy/%s.log {\n\t\t\troll_size 10MiB\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", domain)
	b.WriteString("}\n")
	return b.String()
}

// headerLine — строка внутри блока header: «Имя "значение"» или «-Имя».
func headerLine(h Header) string {
	if h.Value == "-" {
		return "-" + h.Name
	}
	return h.Name + " " + quoteValue(h.Value)
}

// quoteValue берёт значение в кавычки для Caddyfile. Плейсхолдеры вида {uri}
// внутри кавычек Caddy подставляет так же.
func quoteValue(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// denyPattern — закрытый путь. «/api/metrics» закрывает ровно его, а не всё,
// что начинается так же; «/admin/» или «/admin*» — весь раздел.
func denyPattern(p string) string {
	if strings.ContainsAny(p, "*") {
		return p
	}
	if strings.HasSuffix(p, "/") {
		return p + "*"
	}
	return p
}

// pathPattern превращает «/logs/» в «/logs/*», сохраняя уже готовые шаблоны.
func pathPattern(p string) string {
	if strings.ContainsAny(p, "*") {
		return p
	}
	if strings.HasSuffix(p, "/") {
		return p + "*"
	}
	return p + "*"
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}

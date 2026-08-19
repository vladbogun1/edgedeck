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
	fmt.Fprintf(&b, "\n%s {\n", strings.Join(addresses, ", "))

	// Общие для домена вещи берём у первого маршрута: заголовки и HSTS
	// относятся к сайту целиком, а не к отдельному пути.
	head := list[0]
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
			fmt.Fprintf(&b, "\thandle %s {\n\t\trespond \"\" 404\n\t}\n", pathPattern(p))
		}
	}

	for _, r := range list {
		b.WriteString("\n")
		if r.Path == "/" {
			b.WriteString(indent(renderBody(r, domain), "\t"))
		} else {
			fmt.Fprintf(&b, "\thandle %s {\n", pathPattern(r.Path))
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

	if r.Auth != nil && r.Auth.Upstream != "" {
		fmt.Fprintf(&b, "# Требовать вход: спрашиваем у %s, есть ли активная сессия\n", r.Auth.Upstream)
		fmt.Fprintf(&b, "forward_auth %s {\n\turi %s\n\tcopy_headers Cookie\n}\n", r.Auth.Upstream, r.Auth.URI)
	}

	switch r.Type {
	case TypeProxy:
		fmt.Fprintf(&b, "reverse_proxy %s {\n", r.Target.Addr())
		b.WriteString("\theader_up X-Real-IP {remote_host}\n")
		if r.HideUpstreamCSP {
			b.WriteString("\t# Снимаем CSP апстрима: два заголовка браузер объединит по пересечению\n")
			b.WriteString("\t# и страница сломается на первом же расхождении (DESIGN.md §2.11)\n")
			b.WriteString("\theader_down -Content-Security-Policy\n")
		}
		if r.ReadTimeout != "" {
			fmt.Fprintf(&b, "\ttransport http {\n\t\tread_timeout %s\n\t}\n", r.ReadTimeout)
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

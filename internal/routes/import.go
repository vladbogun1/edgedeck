package routes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Импорт сайтов, заведённых руками (через bin/newsite), в routes.yaml.
//
// Разбор нарочно узкий: понимаем только простые файлы — проксирование на
// «контейнер:порт», статику и редирект. Всё остальное честно возвращает ошибку,
// и такой файл остаётся жить руками. Лучше не импортировать, чем импортировать
// неправильно: сгенерированный файл потом перезапишет оригинал.
var (
	blockStartRe = regexp.MustCompile(`(?m)^([^\s#{}(][^{}\n]*?)\s*\{\s*$`)
	proxyLineRe  = regexp.MustCompile(`(?m)^\s*reverse_proxy\s+([^\s{]+)`)
	rootLineRe   = regexp.MustCompile(`(?m)^\s*root\s+\*\s+(\S+)`)
	redirLineRe  = regexp.MustCompile(`(?m)^\s*redir\s+(\S+)`)
	importLineRe = regexp.MustCompile(`(?m)^\s*import\s+(\w+)`)
	handleLineRe = regexp.MustCompile(`(?m)^\s*(handle|handle_path|route)\s`)
	authLineRe   = regexp.MustCompile(`(?m)^\s*(forward_auth|basicauth|basic_auth)\s`)
)

// Import разбирает файл сайта в маршрут.
func Import(body string) (Route, error) {
	clean := stripComments(body)

	if handleLineRe.MatchString(clean) {
		return Route{}, fmt.Errorf("в файле есть разбивка по путям (handle/route) — такое пока импортируется только руками")
	}
	if authLineRe.MatchString(clean) {
		return Route{}, fmt.Errorf("в файле настроена авторизация — перенесите её в маршрут руками, чтобы ничего не потерять")
	}

	blocks := blockStartRe.FindAllStringSubmatch(clean, -1)
	if len(blocks) == 0 {
		return Route{}, fmt.Errorf("не нашёл блока сайта")
	}
	if len(blocks) > 1 {
		return Route{}, fmt.Errorf("в файле несколько блоков — импортируйте руками")
	}

	var names []string
	for _, part := range strings.Split(blocks[0][1], ",") {
		n := strings.TrimSpace(part)
		n = strings.TrimPrefix(strings.TrimPrefix(n, "https://"), "http://")
		if n == "" || !strings.Contains(n, ".") {
			return Route{}, fmt.Errorf("адрес «%s» не домен — импортировать не буду", strings.TrimSpace(part))
		}
		names = append(names, strings.ToLower(n))
	}

	r := Route{
		Domain:  names[0],
		Aliases: names[1:],
		Path:    "/",
		// Что было в файле руками — переносим как есть, ничего не «улучшаем»:
		// набор заголовков берётся из import common, если он там был.
		SecurityHeaders: hasImport(clean, "common"),
		HSTS:            hasImport(clean, "hsts"),
	}

	switch {
	case proxyLineRe.MatchString(clean):
		target := proxyLineRe.FindStringSubmatch(clean)[1]
		host, portStr, ok := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(target, "http://"), "https://"), ":")
		if !ok {
			return Route{}, fmt.Errorf("цель «%s» без порта — допишите порт и повторите", target)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return Route{}, fmt.Errorf("порт «%s» непонятен", portStr)
		}
		r.Type = TypeProxy
		kind := KindContainer
		if host == "host.docker.internal" {
			kind = KindHost
		}
		r.Target = &Target{Kind: kind, Name: host, Port: port}

	case rootLineRe.MatchString(clean):
		r.Type = TypeStatic

	case redirLineRe.MatchString(clean):
		r.Type = TypeRedirect
		r.RedirectTo = strings.TrimSuffix(redirLineRe.FindStringSubmatch(clean)[1], "{uri}")

	case strings.Contains(clean, "respond") && strings.Contains(clean, "404"):
		r.Type = TypeACMEOnly

	default:
		return Route{}, fmt.Errorf("не понял, что делает этот файл")
	}

	if err := r.Validate(); err != nil {
		return Route{}, err
	}
	return r, nil
}

func hasImport(body, name string) bool {
	for _, m := range importLineRe.FindAllStringSubmatch(body, -1) {
		if m[1] == name {
			return true
		}
	}
	return false
}

func stripComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if idx := strings.Index(l, "#"); idx >= 0 {
			lines[i] = l[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

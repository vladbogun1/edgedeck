package routes

import (
	"strings"
	"testing"
)

func TestRenderExtras(t *testing.T) {
	list := []Route{
		{
			Domain: "example.com", Aliases: []string{"www.example.com"}, Path: "/", Type: TypeProxy,
			Target:        &Target{Kind: KindContainer, Name: "app", Port: 80},
			Headers:       []Header{{Name: "Referrer-Policy", Value: "same-origin"}, {Name: "X-Frame-Options", Value: "-"}},
			PathRedirects: []PathRedirect{{From: "/logs", To: "/logs/"}},
			Compress:      true,
			Flush:         true,
			DialTimeout:   "5s",
			DenyPaths:     []string{"/api/metrics"},
		},
		{
			Domain: "example.com", Path: "/logs/", Type: TypeProxy, MaxUpload: "128MB",
			Target: &Target{Kind: KindContainer, Name: "dozzle", Port: 8080},
			Auth:   &ForwardAuth{Upstream: "auth:4000", URI: "/check", HeaderUp: []Header{{Name: "X-Original-URI", Value: "{uri}"}}},
		},
		{Domain: "old.example.com", Path: "/", Type: TypeRedirect, RedirectTo: "https://example.com", ACMEWebroot: "/srv/webroot"},
	}
	out := Render(list)
	site := out["example.com.caddy"]
	for _, want := range []string{
		"example.com, www.example.com {",
		"\tencode zstd gzip\n",
		`Referrer-Policy "same-origin"`,
		"\t\t-X-Frame-Options\n",
		"\tredir /logs /logs/ 301\n",
		"\thandle /api/metrics {\n", // ровно путь, не префикс
		"\thandle /logs/* {\n\t\trequest_body {\n\t\t\tmax_size 128MB",
		`header_up X-Original-URI "{uri}"`,
		"\thandle {\n\t\treverse_proxy app:80 {",
		"flush_interval -1",
		"dial_timeout 5s",
	} {
		if !strings.Contains(site, want) {
			t.Errorf("нет %q в\n%s", want, site)
		}
	}
	if strings.Contains(site, "copy_headers") {
		t.Error("copy_headers не просили — его быть не должно")
	}
	old := out["old.example.com.caddy"]
	for _, want := range []string{
		"http://old.example.com {",
		"root * /srv/webroot",
		"redir https://example.com{uri} permanent",
	} {
		if !strings.Contains(old, want) {
			t.Errorf("нет %q в\n%s", want, old)
		}
	}
}

func TestValidateRejectsInjection(t *testing.T) {
	base := func() Route {
		return Route{Domain: "example.com", Path: "/", Type: TypeStub}
	}
	bad := []func(*Route){
		func(r *Route) { r.Headers = []Header{{Name: "X-A", Value: "ok\n}\nevil {"}} },
		func(r *Route) { r.Headers = []Header{{Name: "X A{", Value: "v"}} },
		func(r *Route) { r.PathRedirects = []PathRedirect{{From: "/a b", To: "/c"}} },
		func(r *Route) { r.ACMEWebroot = "relative/path" },
		func(r *Route) { r.DialTimeout = "5 minutes" },
	}
	for i, f := range bad {
		r := base()
		f(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("случай %d должен быть отклонён", i)
		}
	}
}

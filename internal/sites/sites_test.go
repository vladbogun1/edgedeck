package sites

import "testing"

func TestParseRedirectWithServiceBlocks(t *testing.T) {
	body := `http://old.example.com {
	handle /.well-known/acme-challenge/* {
		root * /srv/certbot-webroot
		file_server
	}
	handle {
		redir https://example.com{uri} permanent
	}
}

old.example.com {
	redir https://example.com{uri} permanent
	handle_errors {
		root * /var/www/_edgedeck
		file_server
	}
}
`
	s := parse("old.example.com.caddy", body)
	if s.Kind != "redirect" {
		t.Fatalf("kind = %q, ждали redirect (цели %v)", s.Kind, s.Targets)
	}
}

func TestParsePathRedirectIsNotSiteRedirect(t *testing.T) {
	body := "example.com {\n\tredir /logs /logs/ 301\n\thandle {\n\t\treverse_proxy app:80\n\t}\n}\n"
	s := parse("example.com.caddy", body)
	if s.Kind != "proxy" {
		t.Fatalf("kind = %q, ждали proxy (цели %v)", s.Kind, s.Targets)
	}
	for _, tg := range s.Targets {
		if tg == "редирект: /logs" {
			t.Fatal("редирект пути попал в цели сайта")
		}
	}
}

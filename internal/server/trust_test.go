package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZoneOf(t *testing.T) {
	cases := map[string]string{
		"www.www.mail.amzcomverif.kozow.com":      "amzcomverif.kozow.com",
		"app.demo.chisetup.com.ua":                "chisetup.com.ua",
		"Bots.Bogun.Site:443":                     "bogun.site",
		"132-145-132-80.cprapid.com":              "cprapid.com",
		"www.www.pivonia.150-136-40-217.sslip.io": "www.www.pivonia.150-136-40-217.sslip.io",
		"app.10.0.0.1.nip.io":                     "app.10.0.0.1.nip.io",
		"150.136.40.217":                          "",
		"[::1]:443":                               "",
		"edgedeck":                                "",
		"":                                        "",
	}
	for in, want := range cases {
		if got := zoneOf(in); got != want {
			t.Errorf("zoneOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrusted(t *testing.T) {
	edge := t.TempDir()
	sitesDir := filepath.Join(edge, "sites")
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	site := "maxsolch.com, pivonia.150-136-40-217.sslip.io {\n\troot * /srv\n\tfile_server\n}\n"
	if err := os.WriteFile(filepath.Join(sitesDir, "maxsolch.caddy"), []byte(site), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newTrustStore(t.TempDir(), edge)

	for host, want := range map[string]bool{
		"132.145.132.80":            true, // заход по IP
		"edgedeck:4000":             true, // внутреннее имя
		"mail.maxsolch.com":         true, // поддомен своей зоны
		"amzcomverif.kozow.com":     false,
		"www.amzcomverif.kozow.com": false,
		"www.google.com":            false,
		// sslip.io: своё — только само имя из маршрута, не соседи и не www.
		"pivonia.150-136-40-217.sslip.io":         true,
		"www.www.pivonia.150-136-40-217.sslip.io": false,
		"evil.150-136-40-217.sslip.io":            false,
	} {
		if got := ts.Trusted(host); got != want {
			t.Errorf("Trusted(%q) = %v, want %v", host, got, want)
		}
	}

	if _, err := ts.Approve("new.example.org", true); err != nil {
		t.Fatal(err)
	}
	if !ts.Trusted("www.example.org") {
		t.Error("одобренная зона должна стать своей")
	}
	if !newTrustStore(filepath.Dir(ts.file), edge).Trusted("example.org") {
		t.Error("одобрение должно пережить перезапуск")
	}
}

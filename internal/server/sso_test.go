package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func testSSO(t *testing.T) *ssoConfig {
	t.Helper()
	t.Setenv("EDGEDECK_SSO_DOMAIN", "bogun.site")
	t.Setenv("EDGEDECK_SSO_LOGIN", "https://bogun.site")
	return loadSSO(fleetConfig{secret: []byte("0123456789abcdef0123456789")}, t.TempDir())
}

func TestSSOToken(t *testing.T) {
	c := testSSO(t)
	tok := c.issue(time.Now())
	if _, ok := c.verify(tok); !ok {
		t.Fatal("свежий токен не принят")
	}
	if _, ok := c.verify(tok + "x"); ok {
		t.Fatal("испорченная подпись принята")
	}
	other := loadSSO(fleetConfig{secret: []byte("другой-секрет-флота-123456")}, t.TempDir())
	if _, ok := other.verify(tok); ok {
		t.Fatal("токен чужого флота принят")
	}
	old := c.issue(time.Now().Add(-time.Hour))
	c.revoke(time.Now().Add(-time.Minute))
	if _, ok := c.verify(old); ok {
		t.Fatal("токен, выданный до «выйти везде», принят")
	}
	if _, ok := c.verify(c.issue(time.Now())); !ok {
		t.Fatal("новый токен после отзыва не принят")
	}
}

func TestSSOReturnAndDomain(t *testing.T) {
	c := testSSO(t)
	for in, want := range map[string]string{
		"https://jarvis.bogun.site/x?y=1": "https://jarvis.bogun.site/x?y=1",
		"https://bogun.site/":             "https://bogun.site/",
		"http://jarvis.bogun.site/":       "",
		"https://evilbogun.site/":         "",
		"https://bogun.site.evil.com/":    "",
		"https://user@jarvis.bogun.site/": "",
		"javascript:alert(1)":             "",
	} {
		if got := c.safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q, ждали %q", in, got, want)
		}
	}
	if c.inDomain("evilbogun.site") || !c.inDomain("s1.bogun.site:443") {
		t.Error("проверка домена")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "jarvis.bogun.site"
	w := httptest.NewRecorder()
	c.setCookie(w, r)
	if ck := w.Result().Cookies(); len(ck) != 1 || ck[0].Domain != "bogun.site" || !ck[0].HttpOnly || !ck[0].Secure {
		t.Fatalf("cookie: %+v", ck)
	}
	r.Host = "mail.maxsolch.com"
	w = httptest.NewRecorder()
	c.setCookie(w, r)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cookie на чужом домене поставлена")
	}
}

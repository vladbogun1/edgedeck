package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Единый вход для своих сервисов (PLAN.md Q-2).
//
// После входа в любую панель (код из Telegram или пароль) ставится cookie
// ssoCookie на весь домен EDGEDECK_SSO_DOMAIN (bogun.site): подписанный общим
// секретом флота токен, проверить который может любая панель без обращения к
// соседям. Маршрут с `sso: true` Caddy сначала проверяет у локальной панели
// (forward_auth → /internal/sso/check); без входа — редирект на страницу входа
// EDGEDECK_SSO_LOGIN с возвратом на исходный адрес.
//
// Токен пускает и в сами панели всех серверов — переключение s1 ↔ s3 без
// повторного входа. Чтобы он не утёк в приложения на поддоменах, генератор
// вырезает эту cookie из запросов ко всем целям, кроме самой панели (render.go).
//
// Отзыв: «Выйти везде» записывает момент выхода (data/sso-not-before) и
// рассылает его соседям по флоту — токены, выданные раньше, перестают
// действовать на всех серверах.

const (
	ssoCookie = "edgedeck_sso"
	ssoTTL    = 30 * 24 * time.Hour
)

type ssoConfig struct {
	domain string // bogun.site
	login  string // https://bogun.site — куда отправлять на вход
	key    []byte
	file   string // момент отзыва

	mu        sync.Mutex
	notBefore time.Time
}

func loadSSO(fleet fleetConfig, dataDir string) *ssoConfig {
	c := &ssoConfig{
		domain: strings.Trim(strings.ToLower(strings.TrimSpace(os.Getenv("EDGEDECK_SSO_DOMAIN"))), "."),
		login:  strings.TrimRight(strings.TrimSpace(os.Getenv("EDGEDECK_SSO_LOGIN")), "/"),
		file:   filepath.Join(dataDir, "sso-not-before"),
	}
	if len(fleet.secret) >= 16 {
		// Отдельный ключ из секрета флота: подпись токена и подпись запросов
		// флота не должны быть взаимозаменяемы.
		h := hmac.New(sha256.New, fleet.secret)
		h.Write([]byte("edgedeck-sso-v1"))
		c.key = h.Sum(nil)
	}
	if b, err := os.ReadFile(c.file); err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b))); err == nil {
			c.notBefore = t
		}
	}
	return c
}

func (c *ssoConfig) enabled() bool { return c != nil && c.domain != "" && len(c.key) > 0 }

// inDomain — хост внутри домена единого входа (сам домен или поддомен).
func (c *ssoConfig) inDomain(host string) bool {
	host = strings.ToLower(host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	return host == c.domain || strings.HasSuffix(host, "."+c.domain)
}

type ssoToken struct {
	Issued  int64 `json:"iat"`
	Expires int64 `json:"exp"`
}

func (c *ssoConfig) sign(payload string) string {
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (c *ssoConfig) issue(now time.Time) string {
	b, _ := json.Marshal(ssoToken{Issued: now.Unix(), Expires: now.Add(ssoTTL).Unix()})
	p := base64.RawURLEncoding.EncodeToString(b)
	return p + "." + c.sign(p)
}

// verify возвращает время выдачи действующего токена.
func (c *ssoConfig) verify(v string) (time.Time, bool) {
	if !c.enabled() {
		return time.Time{}, false
	}
	p, sig, ok := strings.Cut(v, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(c.sign(p))) {
		return time.Time{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return time.Time{}, false
	}
	var t ssoToken
	if json.Unmarshal(b, &t) != nil {
		return time.Time{}, false
	}
	issued := time.Unix(t.Issued, 0)
	c.mu.Lock()
	nb := c.notBefore
	c.mu.Unlock()
	if time.Now().Unix() > t.Expires || issued.Before(nb) {
		return time.Time{}, false
	}
	return issued, true
}

func (c *ssoConfig) fromRequest(r *http.Request) (time.Time, bool) {
	ck, err := r.Cookie(ssoCookie)
	if err != nil {
		return time.Time{}, false
	}
	return c.verify(ck.Value)
}

// setCookie ставит токен, если запрос пришёл на хост внутри домена.
func (c *ssoConfig) setCookie(w http.ResponseWriter, r *http.Request) {
	if !c.enabled() || !c.inDomain(r.Host) {
		return
	}
	now := time.Now()
	http.SetCookie(w, &http.Cookie{
		Name:     ssoCookie,
		Value:    c.issue(now),
		Domain:   c.domain,
		Path:     "/",
		Expires:  now.Add(ssoTTL),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (c *ssoConfig) clearCookie(w http.ResponseWriter) {
	if !c.enabled() {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: ssoCookie, Value: "", Domain: c.domain, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true})
}

// revoke отзывает все токены, выданные до now.
func (c *ssoConfig) revoke(now time.Time) {
	c.mu.Lock()
	if now.After(c.notBefore) {
		c.notBefore = now
	}
	nb := c.notBefore
	c.mu.Unlock()
	_ = os.WriteFile(c.file, []byte(nb.UTC().Format(time.RFC3339)), 0o600)
}

// safeReturn — адрес возврата после входа, только внутри домена и по https.
func (c *ssoConfig) safeReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !c.inDomain(u.Host) || u.User != nil {
		return ""
	}
	return u.String()
}

// handleSSOCheck — для Caddy forward_auth. 200 — пускать; иначе редирект на вход.
// Caddy передаёт исходные заголовки (Cookie) и X-Forwarded-Host/Uri/Proto.
func (a *app) handleSSOCheck(w http.ResponseWriter, r *http.Request) {
	if !a.sso.enabled() {
		http.Error(w, "единый вход не настроен", http.StatusForbidden)
		return
	}
	if _, ok := a.sso.fromRequest(r); ok {
		w.Header().Set("X-EdgeDeck-User", "owner")
		w.WriteHeader(http.StatusOK)
		return
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https"
	}
	back := proto + "://" + r.Header.Get("X-Forwarded-Host") + r.Header.Get("X-Forwarded-Uri")
	login := a.sso.login
	if login == "" {
		login = "https://" + a.sso.domain
	}
	http.Redirect(w, r, login+"/?rd="+url.QueryEscape(back), http.StatusFound)
}

// authed — запрос от вошедшего: своя сессия панели или токен единого входа.
// Возвращает момент входа (для «свежести» при смене пароля).
func (a *app) authed(r *http.Request) (time.Time, bool) {
	if s := a.currentSession(r); s != nil {
		return s.Created, true
	}
	if a.sso.enabled() && a.sso.inDomain(r.Host) {
		return a.sso.fromRequest(r)
	}
	return time.Time{}, false
}

// handleFleetRevoke — сосед сообщает «выйти везде». Только по подписи флота.
func (a *app) handleFleetRevoke(w http.ResponseWriter, r *http.Request) {
	if !a.fleet.checkRequest(r) {
		http.Error(w, "нет", http.StatusForbidden)
		return
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil {
		http.Error(w, "нет времени", http.StatusBadRequest)
		return
	}
	a.sso.revoke(time.Unix(t, 0))
	a.auth.DropAll()
	a.state.Audit("logout_all", "флот", "выход везде по сигналу соседа", nil)
	w.WriteHeader(http.StatusNoContent)
}

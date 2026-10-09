package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vladbogun1/edgedeck/internal/auth"
)

// Вход по коду из Telegram.
//
// Пароль забывается, а бот уже есть и пишет в личный чат владельца (проверено:
// чат типа private). Поэтому основной вход — одноразовый код туда же:
//   - код из 6 цифр живёт 5 минут и привязан к «вызову» — случайному id, который
//     получает только браузер, запросивший код;
//   - на один вызов 5 попыток, плюс общий с паролем счётчик неудач по адресу
//     (5 за 15 минут) — перебрать миллион вариантов нельзя;
//   - живых вызовов не больше трёх: кто-то, нажимающий «прислать код» в цикле,
//     не сможет вытеснить ваш собственный;
//   - отправка не чаще раза в 20 секунд с адреса и 10 раз в час всего — чтобы
//     чат нельзя было завалить;
//   - в сообщении видно, с какого адреса и домена запросили код: если запрос
//     не ваш, это сразу заметно.

const (
	otpTTL       = 5 * time.Minute
	otpTries     = 5
	otpLive      = 3
	otpPerIPGap  = 20 * time.Second
	otpPerHour   = 10
	otpDigits    = 6
	challengeLen = 16
)

type otpChallenge struct {
	code    string
	expires time.Time
	tries   int
}

type otpStore struct {
	mu         sync.Mutex
	challenges map[string]*otpChallenge
	lastByIP   map[string]time.Time
	sent       []time.Time
}

func newOTPStore() *otpStore {
	return &otpStore{challenges: map[string]*otpChallenge{}, lastByIP: map[string]time.Time{}}
}

var errOTPRate = fmt.Errorf("код уже отправлен — подождите немного")

// issue создаёт вызов и код. Ошибка — превышен лимит отправки.
func (s *otpStore) issue(ip string) (id, code string, err error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if t, ok := s.lastByIP[ip]; ok && now.Sub(t) < otpPerIPGap {
		return "", "", errOTPRate
	}
	if len(s.sent) >= otpPerHour {
		return "", "", fmt.Errorf("слишком много кодов за час — попробуйте позже или войдите паролем")
	}
	// Места нет — уступает самый старый вызов.
	for len(s.challenges) >= otpLive {
		var oldestID string
		var oldest time.Time
		for k, c := range s.challenges {
			if oldestID == "" || c.expires.Before(oldest) {
				oldestID, oldest = k, c.expires
			}
		}
		delete(s.challenges, oldestID)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", "", err
	}
	raw := make([]byte, challengeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	id, code = hex.EncodeToString(raw), fmt.Sprintf("%0*d", otpDigits, n.Int64())
	s.challenges[id] = &otpChallenge{code: code, expires: now.Add(otpTTL)}
	s.lastByIP[ip] = now
	s.sent = append(s.sent, now)
	return id, code, nil
}

// drop забывает вызов: сообщение не ушло, и код в нём никому не известен.
func (s *otpStore) drop(id string) {
	s.mu.Lock()
	delete(s.challenges, id)
	s.mu.Unlock()
}

// check сверяет код. Удачный вызов сгорает, исчерпанный — тоже.
func (s *otpStore) check(id, code string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	c, ok := s.challenges[id]
	if !ok {
		return false
	}
	c.tries++
	good := subtle.ConstantTimeCompare([]byte(c.code), []byte(code)) == 1
	if good || c.tries >= otpTries {
		delete(s.challenges, id)
	}
	return good
}

func (s *otpStore) pruneLocked(now time.Time) {
	for k, c := range s.challenges {
		if now.After(c.expires) {
			delete(s.challenges, k)
		}
	}
	cut := now.Add(-time.Hour)
	kept := s.sent[:0]
	for _, t := range s.sent {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	s.sent = kept
	for ip, t := range s.lastByIP {
		if now.Sub(t) > otpPerIPGap {
			delete(s.lastByIP, ip)
		}
	}
}

// handleLoginMethods — каким способом можно войти. Только «есть ли Telegram»,
// ничего о сервере.
func (a *app) handleLoginMethods(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"telegram": a.notify.Enabled()})
}

func (a *app) handleOTPSend(w http.ResponseWriter, r *http.Request) {
	if !a.notify.Enabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Telegram не настроен — войдите паролем"})
		return
	}
	ip := clientIP(r)
	if a.auth.Locked(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "слишком много попыток — подождите 15 минут"})
		return
	}
	id, code, err := a.otp.issue(ip)
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	text := fmt.Sprintf("🔐 Код входа в EdgeDeck на <b>%s</b>: <code>%s</code>\n"+
		"Запросили с адреса %s, домен %s. Код действует 5 минут.\n"+
		"Если это не вы — просто не вводите его.",
		hostname(a.cfg.HostProcDir), code, ip, htmlEscape(r.Host))
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := a.notify.Send(ctx, text); err != nil {
		a.otp.drop(id)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "не смог отправить код: " + err.Error()})
		return
	}
	a.state.Audit("otp_sent", ip, "отправлен код входа в Telegram", map[string]any{"host": r.Host})
	writeJSON(w, http.StatusOK, map[string]any{"challenge": id})
}

func (a *app) handleOTPLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не разобрал запрос"})
		return
	}
	ip := clientIP(r)
	if a.auth.Locked(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "слишком много попыток — подождите 15 минут"})
		return
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, req.Code)
	if !a.otp.check(req.Challenge, code) {
		a.auth.Fail(ip)
		a.state.Audit("login_failed", ip, "неверный код из Telegram", map[string]any{"host": r.Host})
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "код неверный или истёк — запросите новый"})
		return
	}
	sess, err := a.auth.Issue(ip, r.UserAgent())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.startSession(w, r, sess)
	a.state.Audit("login", ip, "вход в панель по коду из Telegram", map[string]any{"host": r.Host})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// startSession ставит куку сессии — общее для входа паролем и кодом.
func (a *app) startSession(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.Expires,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

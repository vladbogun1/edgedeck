// Package auth — вход по паролю и сессии.
//
// Форма входа видна на любом ненастроенном домене (DESIGN.md §9.0), поэтому:
//   - пароль хранится только как argon2id-хеш;
//   - до входа наружу не отдаётся ничего, даже имени сервера;
//   - попытки ограничены по адресу, и об этом пишется в журнал.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Параметры argon2id. Сервер маленький (2 ядра), поэтому память умеренная:
// 64 МБ на одну проверку пароля, чтобы вход не подвешивал панель.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

var (
	ErrBadPassword = errors.New("неверный пароль")
	ErrLocked      = errors.New("слишком много попыток")
)

type Session struct {
	Token     string    `json:"token"`
	Created   time.Time `json:"created"`
	Expires   time.Time `json:"expires"`
	LastSeen  time.Time `json:"last_seen"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
}

type Store struct {
	passwordFile string
	sessionsFile string
	ttl          time.Duration

	mu       sync.Mutex
	hash     string
	sessions map[string]*Session
	attempts map[string]*attemptCounter
}

type attemptCounter struct {
	count int
	until time.Time
}

// Ограничение: 5 попыток за 15 минут с одного адреса, затем пауза 15 минут.
const (
	maxAttempts      = 5
	attemptWindowLen = 15 * time.Minute
)

func NewStore(passwordFile, sessionsFile string, ttl time.Duration) (*Store, error) {
	s := &Store{
		passwordFile: passwordFile,
		sessionsFile: sessionsFile,
		ttl:          ttl,
		sessions:     map[string]*Session{},
		attempts:     map[string]*attemptCounter{},
	}
	if b, err := os.ReadFile(passwordFile); err == nil {
		s.hash = strings.TrimSpace(string(b))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("файл пароля: %w", err)
	}
	s.loadSessions()
	return s, nil
}

// HasPassword говорит, настроен ли пароль. Если нет — панель на старте создаст его.
func (s *Store) HasPassword() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hash != ""
}

// SetPassword записывает новый пароль. Все сессии остаются жить: смена пароля
// не должна выкидывать того, кто её и делает. Для «выйти везде» есть DropAll.
func (s *Store) SetPassword(password string) error {
	if len(password) < 8 {
		return errors.New("пароль короче 8 символов")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := fmt.Sprintf("argon2id$%d$%d$%d$%s$%s",
		argonTime, argonMemory, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))

	s.mu.Lock()
	s.hash = enc
	s.mu.Unlock()
	return os.WriteFile(s.passwordFile, []byte(enc+"\n"), 0o600)
}

// Login проверяет пароль и выдаёт сессию.
func (s *Store) Login(password, ip, userAgent string) (*Session, error) {
	s.mu.Lock()
	hash := s.hash
	if w := s.attempts[ip]; w != nil && w.count >= maxAttempts && time.Now().Before(w.until) {
		s.mu.Unlock()
		return nil, ErrLocked
	}
	s.mu.Unlock()

	if !verify(hash, password) {
		s.Fail(ip)
		return nil, ErrBadPassword
	}

	return s.Issue(ip, userAgent)
}

// Verify проверяет пароль, не выдавая сессии: для смены пароля изнутри панели.
// Неудачи идут в тот же счётчик, что и у входа.
func (s *Store) Verify(password, ip string) error {
	if s.Locked(ip) {
		return ErrLocked
	}
	s.mu.Lock()
	hash := s.hash
	s.mu.Unlock()
	if !verify(hash, password) {
		s.Fail(ip)
		return ErrBadPassword
	}
	return nil
}

// FreshWindow — сколько после входа можно сменить пароль, не вводя текущий.
// Вход сам по себе подтвердил, что это вы (паролем или кодом из Telegram), а
// без этого забытый пароль было бы не сменить вовсе.
const FreshWindow = 15 * time.Minute

// Fresh — сессия создана только что.
func (s *Session) Fresh() bool { return time.Since(s.Created) < FreshWindow }

// Issue выдаёт сессию тому, кто уже доказал, что он свой: паролем или кодом из
// Telegram. Счётчик неудачных попыток с этого адреса сбрасывается.
func (s *Store) Issue(ip, userAgent string) (*Session, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sess := &Session{
		Token:     token,
		Created:   now,
		Expires:   now.Add(s.ttl),
		LastSeen:  now,
		UserAgent: trim(userAgent, 200),
		IP:        ip,
	}
	s.mu.Lock()
	delete(s.attempts, ip)
	s.sessions[token] = sess
	s.saveSessionsLocked()
	s.mu.Unlock()
	return sess, nil
}

// Locked — адрес исчерпал попытки входа (общий счётчик для пароля и кода).
func (s *Store) Locked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.attempts[ip]
	return w != nil && w.count >= maxAttempts && time.Now().Before(w.until)
}

// Fail засчитывает неудачную попытку входа с адреса.
func (s *Store) Fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.attempts[ip]
	if w == nil || time.Now().After(w.until) {
		w = &attemptCounter{until: time.Now().Add(attemptWindowLen)}
		s.attempts[ip] = w
	}
	w.count++
}

// Check возвращает сессию по токену, если она жива.
func (s *Store) Check(token string) (*Session, bool) {
	if token == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.Expires) {
		delete(s.sessions, token)
		s.saveSessionsLocked()
		return nil, false
	}
	// Отметку времени пишем не чаще раза в минуту, чтобы не дёргать диск на каждый запрос.
	if time.Since(sess.LastSeen) > time.Minute {
		sess.LastSeen = time.Now()
		s.saveSessionsLocked()
	}
	return sess, true
}

func (s *Store) Drop(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.saveSessionsLocked()
	s.mu.Unlock()
}

func (s *Store) DropAll() {
	s.mu.Lock()
	s.sessions = map[string]*Session{}
	s.saveSessionsLocked()
	s.mu.Unlock()
}

// Sessions — список активных сессий для экрана настроек.
func (s *Store) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		c := *sess
		c.Token = c.Token[:8] + "…" // наружу целиком не отдаём
		out = append(out, c)
	}
	return out
}

func (s *Store) loadSessions() {
	b, err := os.ReadFile(s.sessionsFile)
	if err != nil {
		return
	}
	var list []*Session
	if json.Unmarshal(b, &list) != nil {
		return
	}
	now := time.Now()
	for _, sess := range list {
		if sess != nil && now.Before(sess.Expires) {
			s.sessions[sess.Token] = sess
		}
	}
}

func (s *Store) saveSessionsLocked() {
	list := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	tmp := s.sessionsFile + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.sessionsFile)
	}
}

func verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "argon2id" {
		return false
	}
	var t, m, p int
	if _, err := fmt.Sscanf(parts[1]+" "+parts[2]+" "+parts[3], "%d %d %d", &t, &m, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// RandomPassword — стартовый пароль, если пользователь не задал свой.
// Печатается в лог один раз, чтобы панель никогда не стояла без пароля.
func RandomPassword() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "edgedeck-" + time.Now().Format("20060102-150405")
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

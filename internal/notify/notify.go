// Package notify — уведомления в Telegram.
//
// Смысл ровно один: на сервере 1 продление сертификатов не работало месяцами, и
// никто не знал (DESIGN.md §2.7). Молчаливо сломанная автоматизация хуже
// отсутствующей, поэтому панель обязана уметь сказать вслух.
//
// О чём говорим и о чём молчим:
//   - цель не отвечает дольше пяти минут — говорим один раз, и один раз о
//     возвращении. Без этого «моргающий» контейнер завалит чат;
//   - сертификат кончается — раз в сутки, начиная с 14 дней;
//   - новый домен пришёл на сервер — один раз на домен;
//   - вход в панель и откат — сразу: это события, которые хочется видеть.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Token  string
	ChatID string
}

func LoadConfig() Config {
	return Config{
		Token:  strings.TrimSpace(os.Getenv("EDGEDECK_TELEGRAM_TOKEN")),
		ChatID: strings.TrimSpace(os.Getenv("EDGEDECK_TELEGRAM_CHAT")),
	}
}

func (c Config) Enabled() bool { return c.Token != "" && c.ChatID != "" }

type Notifier struct {
	cfg  Config
	http *http.Client

	mu   sync.Mutex
	sent map[string]time.Time
}

func New(cfg Config) *Notifier {
	return &Notifier{
		cfg:  cfg,
		http: &http.Client{Timeout: 15 * time.Second},
		sent: map[string]time.Time{},
	}
}

func (n *Notifier) Enabled() bool { return n.cfg.Enabled() }

// Send отправляет сообщение. Ошибку не глотаем: она нужна кнопке «проверить связь».
func (n *Notifier) Send(ctx context.Context, text string) error {
	if !n.cfg.Enabled() {
		return fmt.Errorf("уведомления не настроены: задайте EDGEDECK_TELEGRAM_TOKEN и EDGEDECK_TELEGRAM_CHAT в ~/edgedeck/.env")
	}
	body, _ := json.Marshal(map[string]any{
		"chat_id":                  n.cfg.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(n.cfg.Token) + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("не дошло до Telegram: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram ответил %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Once отправляет сообщение не чаще, чем раз в cooldown для данного ключа.
// Так «цель не отвечает» не превращается в поток одинаковых сообщений.
func (n *Notifier) Once(ctx context.Context, key string, cooldown time.Duration, text string) {
	if !n.cfg.Enabled() {
		return
	}
	n.mu.Lock()
	last, seen := n.sent[key]
	if seen && time.Since(last) < cooldown {
		n.mu.Unlock()
		return
	}
	n.sent[key] = time.Now()
	n.mu.Unlock()

	if err := n.Send(ctx, text); err != nil {
		slog.Warn("уведомление не отправилось", "key", key, "err", err)
		// Не запоминаем неудачу: пусть попробует снова.
		n.mu.Lock()
		delete(n.sent, key)
		n.mu.Unlock()
	}
}

// Forget убирает отметку — нужно, чтобы после возвращения сайта следующее
// падение снова считалось новостью.
func (n *Notifier) Forget(key string) {
	n.mu.Lock()
	delete(n.sent, key)
	n.mu.Unlock()
}

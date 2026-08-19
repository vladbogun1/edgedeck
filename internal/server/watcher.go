package server

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vladbogun1/edgedeck/internal/routes"
)

// Сторож: раз в минуту смотрит на сайты и сертификаты и сообщает, если что-то
// не так. Существует потому, что тишина — плохой признак состояния (DESIGN.md §2.7).
//
// Пороги выбраны так, чтобы уведомление означало «пойди посмотри», а не «я живой»:
//   - цель молчит 5 минут подряд (одна проверка может не повезти);
//   - сертификат кончается через 14 дней (Caddy продлевает за 30 — значит
//     что-то мешает уже две недели);
//   - домен пришёл на сервер и не настроен — один раз на домен.

const (
	watchInterval  = time.Minute
	downThreshold  = 5 * time.Minute
	certWarnDays   = 14
	repeatCooldown = 12 * time.Hour
)

type watchState struct {
	mu        sync.Mutex
	downSince map[string]time.Time
	told      map[string]bool
}

func newWatchState() *watchState {
	return &watchState{downSince: map[string]time.Time{}, told: map[string]bool{}}
}

func (a *app) runWatcher(ctx context.Context) {
	// Первый прогон — через минуту после старта: на свежем старте Caddy ещё
	// раскладывает сертификаты, и ранние проверки врут.
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.watchOnce(ctx)
		}
	}
}

func (a *app) watchOnce(ctx context.Context) {
	if !a.notify.Enabled() {
		return
	}
	a.checkSites(ctx)
	a.checkCerts(ctx)
	a.checkUnconfigured(ctx)
	a.checkSelf(ctx)
}

// checkSites опрашивает каждый домен через сам Caddy и держит счётчик времени
// падения: сообщение уходит, только если молчание длится дольше порога.
func (a *app) checkSites(ctx context.Context) {
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		return
	}
	states := a.applier.ProbeDomains(ctx, cfg)
	now := time.Now()

	a.watch.mu.Lock()
	defer a.watch.mu.Unlock()
	for domain, state := range states {
		ok := !strings.HasPrefix(state, "ошибка") && !strings.HasPrefix(state, "5")
		if ok {
			if since, was := a.watch.downSince[domain]; was {
				delete(a.watch.downSince, domain)
				if a.watch.told["down:"+domain] {
					delete(a.watch.told, "down:"+domain)
					a.notify.Forget("down:" + domain)
					a.notify.Once(ctx, "up:"+domain, time.Minute,
						fmt.Sprintf("✅ <b>%s</b> снова отвечает\nлежал %s", domain, humanDuration(now.Sub(since))))
				}
			}
			continue
		}
		since, was := a.watch.downSince[domain]
		if !was {
			a.watch.downSince[domain] = now
			continue
		}
		if now.Sub(since) >= downThreshold && !a.watch.told["down:"+domain] {
			a.watch.told["down:"+domain] = true
			a.notify.Once(ctx, "down:"+domain, repeatCooldown,
				fmt.Sprintf("🔴 <b>%s</b> не отвечает %s\nответ прокси: %s", domain, humanDuration(now.Sub(since)), state))
		}
	}
}

func (a *app) checkCerts(ctx context.Context) {
	certs, err := a.caddy.Certificates()
	if err != nil {
		return
	}
	for _, c := range certs {
		if c.SelfSign || c.DaysLeft > certWarnDays {
			continue
		}
		name := strings.Join(c.Domains, ", ")
		a.notify.Once(ctx, "cert:"+name, 24*time.Hour,
			fmt.Sprintf("🟡 сертификат <b>%s</b> кончается через %d дн.\nCaddy продлевает за 30 дней — значит продлению что-то мешает. Проверьте, что домен всё ещё смотрит на этот сервер",
				name, c.DaysLeft))
	}
}

// checkUnconfigured — домен направили на сервер, а маршрута нет. Сообщаем один
// раз: это либо новый сайт, который вы заводите, либо чей-то чужой домен.
func (a *app) checkUnconfigured(ctx context.Context) {
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		return
	}
	configured := map[string]bool{}
	for _, r := range cfg.Routes {
		configured[r.Domain] = true
		for _, al := range r.Aliases {
			configured[al] = true
		}
	}
	var names []string
	for _, r := range cfg.Routes {
		names = append(names, r.Domain)
	}
	for _, d := range a.state.Seen(names) {
		if configured[d.Host] || d.Hits < 3 {
			continue
		}
		// Домен, который смотрит не на нас, — это сканер с подставленным Host
		// (в списке постоянно висит www.google.com), а не сайт, который вы
		// собираетесь настроить. Про такие молчим: в панели они видны, а
		// сообщение о них было бы шумом.
		if d.TLSAsked && !d.TLSAllowed {
			continue
		}
		a.notify.Once(ctx, "seen:"+d.Host, 7*24*time.Hour,
			fmt.Sprintf("🔵 на сервер пришёл домен <b>%s</b> без маршрута, и его A-запись смотрит сюда.\nЗайдите в панель и настройте его — или забудьте в списке", d.Host))
	}
}

// checkSelf проверяет то, без чего панель бесполезна: связь с Caddy и свежесть
// снимка правил фаервола.
func (a *app) checkSelf(ctx context.Context) {
	if alive, msg := a.caddy.Alive(ctx); !alive {
		a.notify.Once(ctx, "caddy-down", repeatCooldown, "🔴 панель не видит admin API Caddy\n"+msg)
	} else {
		a.notify.Forget("caddy-down")
	}
}

// Notify — то, что панель говорит о собственных действиях, сразу.
func (a *app) notifyEvent(kind, text string) {
	if !a.notify.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	go func() {
		defer cancel()
		if err := a.notify.Send(ctx, text); err != nil {
			slog.Debug("уведомление о событии не ушло", "kind", kind, "err", err)
		}
	}()
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "меньше минуты"
	case d < time.Hour:
		return fmt.Sprintf("%d мин.", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d ч. %d мин.", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d дн.", int(d.Hours()/24))
	}
}

// sortedKeys — для стабильного порядка в сообщениях.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

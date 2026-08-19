package server

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/vladbogun1/edgedeck/internal/routes"
)

// Самолечение привязок к сети edge.
//
// Задача, из-за которой это существует. Прокси находит контейнер по имени в сети
// edge и резолвит его на каждый запрос — поэтому пересоздание контейнера с новым
// IP само по себе ничего не ломает. Но `docker compose up --force-recreate`
// поднимает контейнер только с теми сетями, которые перечислены в ЕГО
// compose-файле. Подключение, сделанное панелью через docker network connect,
// при этом теряется — и маршрут начинает отдавать 502 навсегда, пока кто-то не
// подключит сеть заново. Ровно то, чего делать руками каждый раз никто не будет.
//
// Поэтому раз в 30 секунд панель сверяет: у каждого маршрута с целью-контейнером
// этот контейнер должен быть в сети edge. Если нет — подключает молча и пишет в
// журнал. Чужие compose-файлы при этом не трогаются: панель по-прежнему умеет
// ровно одну операцию записи в докер.
//
// Постоянное решение — строка networks в compose самого приложения; панель её
// показывает. Но пока её нет, сайт не должен лежать.

const reconcileInterval = 30 * time.Second

func (a *app) runReconciler(ctx context.Context) {
	// Первый проход сразу: панель могла перезапуститься как раз после редеплоя.
	a.reconcileOnce(ctx)

	t := time.NewTicker(reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.reconcileOnce(ctx)
		}
	}
}

func (a *app) reconcileOnce(ctx context.Context) {
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil || len(cfg.Routes) == 0 {
		return
	}

	// Какие контейнеры нужны маршрутам.
	needed := map[string]bool{}
	for _, r := range cfg.Routes {
		if r.Type != routes.TypeProxy || r.Target == nil || r.Target.Kind != routes.KindContainer {
			continue
		}
		if r.Target.Name != "" {
			needed[r.Target.Name] = true
		}
	}
	if len(needed) == 0 {
		return
	}

	list, err := a.docker.Containers(ctx)
	if err != nil {
		return
	}
	for _, c := range list {
		if !needed[c.Name] || c.InEdge {
			continue
		}
		// Контейнер вне сетей вообще (host/none) подключить нельзя — там нужна
		// другая цель, и об этом панель говорит на экране «Цели».
		if len(c.Networks) == 0 {
			continue
		}
		if err := a.docker.ConnectToEdge(ctx, c.Name); err != nil {
			slog.Warn("не смог вернуть контейнер в сеть edge", "container", c.Name, "err", err)
			continue
		}
		slog.Info("вернул контейнер в сеть edge после пересоздания", "container", c.Name)
		a.state.Audit("network_healed", "панель",
			c.Name+" остался без сети edge (скорее всего пересоздан) — подключил обратно", nil)
		a.notifyEvent("network_healed",
			"🔧 <b>"+c.Name+"</b> потерял сеть edge после пересоздания — подключил обратно, сайт снова работает."+
				"\nЧтобы это не повторялось, добавьте сеть в его docker-compose.yml:"+
				"\n<pre>networks: [edge]</pre>")
	}
}

// composeSnippet — что дописать в compose приложения, чтобы подключение к сети
// переживало пересоздание контейнера. Показывается в интерфейсе рядом с целью.
func composeSnippet(edgeNetwork string) string {
	return strings.Join([]string{
		"services:",
		"  <ваш сервис>:",
		"    networks: [" + edgeNetwork + "]",
		"",
		"networks:",
		"  " + edgeNetwork + ":",
		"    external: true",
	}, "\n")
}

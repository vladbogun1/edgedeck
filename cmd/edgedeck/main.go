// EdgeDeck — панель маршрутизации доменов и портов поверх Caddy.
//
// Один бинарник, два режима:
//
//	edgedeck            панель (HTTP на :4000, наружу не публикуется)
//	edgedeck --agent    сайдкар с доступом к docker.sock (:4001, белый список)
//
// Почему один бинарник: агент — это 150 строк, отдельный язык и отдельный образ
// ради них не нужны. Почему вообще агент: панели нельзя давать docker.sock целиком,
// иначе её взлом = запуск любых контейнеров на сервере (см. DESIGN.md §4).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/vladbogun1/edgedeck/internal/agent"
	"github.com/vladbogun1/edgedeck/internal/config"
	"github.com/vladbogun1/edgedeck/internal/fwdump"
	"github.com/vladbogun1/edgedeck/internal/server"
	"github.com/vladbogun1/edgedeck/internal/version"
)

func main() {
	agentMode := flag.Bool("agent", false, "запустить сайдкар доступа к Docker вместо панели")
	firewallMode := flag.Bool("firewall", false, "запустить сборщик правил фаервола (только чтение, никуда не слушает)")
	planMode := flag.Bool("plan", false, "показать, что изменит routes.yaml, и проверить конфиг в Caddy (ничего не записывает)")
	applyMode := flag.Bool("apply", false, "применить routes.yaml: снимок, запись, контроль, откат при поломке")
	adoptList := flag.String("adopt", "", "файлы руками через запятую, которые разрешено заменить сгенерированными")
	reason := flag.String("reason", "", "что меняем — попадёт в снимок и журнал")
	discardMode := flag.Bool("discard", false, "вернуть routes.yaml к последнему применённому (правка сохранится в data/rejected)")
	actor := flag.String("actor", "", "кто применяет — для журнала, например agent:commerce-engine (по умолчанию ssh)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *agentMode {
		cfg := agent.LoadConfig()
		log.Info("edgedeck agent", "version", version.Version, "listen", cfg.Listen, "socket", cfg.DockerSocket)
		if err := agent.Run(ctx, cfg); err != nil {
			log.Error("агент остановился с ошибкой", "err", err)
			os.Exit(1)
		}
		return
	}

	if *firewallMode {
		fwCfg := fwdump.LoadConfig()
		log.Info("edgedeck firewall dump", "version", version.Version, "out", fwCfg.OutFile)
		if err := fwdump.Run(ctx, fwCfg); err != nil {
			log.Error("сборщик правил остановился с ошибкой", "err", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Error("конфигурация не загрузилась", "err", err)
		os.Exit(1)
	}
	if *planMode || *applyMode || *discardMode {
		var adopt []string
		for _, f := range strings.Split(*adoptList, ",") {
			if f = strings.TrimSpace(f); f != "" {
				adopt = append(adopt, f)
			}
		}
		mode := server.CLIPlan
		switch {
		case *discardMode:
			mode = server.CLIDiscard
		case *applyMode:
			mode = server.CLIApply
		}
		opts := server.CLIOptions{Mode: mode, Adopt: adopt, Reason: *reason, Actor: *actor}
		if err := server.RunCLI(ctx, cfg, opts, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "ошибка:", err)
			os.Exit(1)
		}
		return
	}

	log.Info("edgedeck", "version", version.Version, "listen", cfg.Listen, "edge_dir", cfg.EdgeDir)
	if err := server.Run(ctx, cfg); err != nil {
		log.Error("панель остановилась с ошибкой", "err", err)
		os.Exit(1)
	}
}

package server

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/vladbogun1/edgedeck/internal/apply"
	"github.com/vladbogun1/edgedeck/internal/caddyx"
	"github.com/vladbogun1/edgedeck/internal/config"
	"github.com/vladbogun1/edgedeck/internal/notify"
	"github.com/vladbogun1/edgedeck/internal/routes"
	"github.com/vladbogun1/edgedeck/internal/state"
)

// Применение routes.yaml из командной строки — для работы по SSH, когда в
// панель не войти или правка сделана руками в файле:
//
//	docker exec edgedeck edgedeck --plan    [--adopt a.caddy,b.caddy]
//	docker exec edgedeck edgedeck --apply   [--adopt …] [--reason "…"] [--actor agent:имя]
//	docker exec edgedeck edgedeck --discard [--actor …]
//
// Путь тот же, что у кнопки в панели: проверка маршрутов, проверка конфига
// самим Caddy до записи, снимок, запись, контроль доменов и откат, если что-то
// перестало отвечать (откат возвращает и routes.yaml). --adopt — файлы,
// написанные руками, которые разрешено заменить сгенерированными. Применения и
// откаты пишутся в журнал «История» и уходят в Telegram, как из панели.
type CLIMode int

const (
	CLIPlan CLIMode = iota
	CLIApply
	CLIDiscard
)

type CLIOptions struct {
	Mode   CLIMode
	Adopt  []string
	Reason string
	Actor  string
}

func RunCLI(ctx context.Context, cfg config.Config, o CLIOptions, out io.Writer) error {
	if o.Actor == "" {
		o.Actor = "ssh"
	}
	ap := &apply.Applier{
		EdgeDir:     cfg.EdgeDir,
		SnapshotDir: filepath.Join(cfg.DataDir, "snapshots"),
		Caddy:       caddyx.New(cfg.CaddyAdminURL, cfg.CaddyDataDir, filepath.Join(cfg.EdgeDir, "certs")),
		CaddyHost:   caddyHostName(cfg.CaddyAdminURL),
		AppliedDir:  filepath.Join(cfg.DataDir, "applied"),
	}
	if err := ap.EnsureApplied(); err != nil {
		return fmt.Errorf("копия применённого routes.yaml: %w", err)
	}
	st := state.NewStore(cfg.SeenDomainsFile(), cfg.AuditFile())
	tg := notify.New(notify.LoadConfig())
	say := func(text string) {
		if !tg.Enabled() {
			return
		}
		c, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		_ = tg.Send(c, text)
	}

	if o.Mode == CLIDiscard {
		kept, err := ap.Discard()
		if err != nil {
			return err
		}
		msg := "routes.yaml возвращён к применённому"
		if kept != "" {
			msg += "; правка сохранена в " + kept
		}
		st.Audit("discard", o.Actor, msg, nil)
		fmt.Fprintln(out, msg)
		return nil
	}

	if d, err := ap.CheckDrift(); err == nil && !d.Empty() {
		if s := d.Summary(); s != "" {
			fmt.Fprintf(out, "в routes.yaml неприменённые правки: %s\n", s)
		}
		if len(d.Files) > 0 {
			fmt.Fprintf(out, "файлы сайтов правили руками (перезапишутся при применении): %s\n", strings.Join(d.Files, ", "))
		}
	}

	rc, err := routes.Load(cfg.EdgeDir)
	if err != nil {
		return err
	}
	for i := range rc.Routes {
		if err := rc.Routes[i].Validate(); err != nil {
			return fmt.Errorf("маршрут %s: %w", rc.Routes[i].ID(), err)
		}
	}
	set := map[string]bool{}
	for _, f := range o.Adopt {
		set[f] = true
	}

	plan := ap.Plan(ctx, rc, set)
	for _, c := range plan.Changes {
		if c.Kind != "unchanged" {
			fmt.Fprintf(out, "%-8s %s\n", c.Kind, c.File)
		}
	}
	for _, w := range plan.Warnings {
		fmt.Fprintf(out, "внимание: %s\n", w)
	}
	if !plan.Valid {
		return fmt.Errorf("конфиг не прошёл проверку Caddy: %s", plan.Error)
	}
	fmt.Fprintln(out, "конфиг проверен Caddy — синтаксис в порядке")
	if o.Mode != CLIApply {
		return nil
	}

	if o.Reason == "" {
		o.Reason = "применение из командной строки"
	}
	res, err := ap.Apply(ctx, rc, apply.Options{Actor: o.Actor, Reason: o.Reason, Adopt: o.Adopt})
	if err != nil {
		st.Audit("apply_failed", o.Actor, o.Reason+" — не вышло: "+err.Error(), nil)
		say("🔴 не применилось (" + o.Actor + "): " + o.Reason + "\n" + err.Error())
		return err
	}
	kind := "apply"
	if res.RolledBack {
		kind = "rollback_auto"
	}
	st.Audit(kind, o.Actor, o.Reason+" — "+res.Message, map[string]any{"snapshot": res.Snapshot})
	if res.RolledBack {
		say("🟠 <b>откат</b> (" + o.Actor + "): " + o.Reason + "\n" + res.Message)
	} else {
		say("🟢 " + o.Reason + " (" + o.Actor + ")\n" + res.Message)
	}

	fmt.Fprintln(out, res.Message)
	if res.Snapshot != "" {
		fmt.Fprintf(out, "снимок: %s\n", res.Snapshot)
	}
	for d, s := range res.After {
		fmt.Fprintf(out, "  %-40s было: %-20s стало: %s\n", d, strings.TrimSpace(res.Before[d]), s)
	}
	if res.RolledBack {
		return fmt.Errorf("изменения откатились")
	}
	return nil
}

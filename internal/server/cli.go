package server

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/vladbogun1/edgedeck/internal/apply"
	"github.com/vladbogun1/edgedeck/internal/caddyx"
	"github.com/vladbogun1/edgedeck/internal/config"
	"github.com/vladbogun1/edgedeck/internal/routes"
)

// Применение routes.yaml из командной строки — для работы по SSH, когда в
// панель не войти или правка сделана руками в файле:
//
//	docker exec edgedeck edgedeck --plan  [--adopt a.caddy,b.caddy]
//	docker exec edgedeck edgedeck --apply [--adopt a.caddy,b.caddy] [--reason "…"]
//
// Путь тот же, что у кнопки в панели: проверка маршрутов, проверка конфига
// самим Caddy до записи, снимок, запись, контроль доменов и откат, если что-то
// перестало отвечать. --adopt — файлы, написанные руками, которые разрешено
// заменить сгенерированными (без него панель их не трогает никогда).
func RunCLI(ctx context.Context, cfg config.Config, doApply bool, adopt []string, reason string, out io.Writer) error {
	rc, err := routes.Load(cfg.EdgeDir)
	if err != nil {
		return err
	}
	for i := range rc.Routes {
		if err := rc.Routes[i].Validate(); err != nil {
			return fmt.Errorf("маршрут %s: %w", rc.Routes[i].ID(), err)
		}
	}
	ap := &apply.Applier{
		EdgeDir:     cfg.EdgeDir,
		SnapshotDir: filepath.Join(cfg.DataDir, "snapshots"),
		Caddy:       caddyx.New(cfg.CaddyAdminURL, cfg.CaddyDataDir, filepath.Join(cfg.EdgeDir, "certs")),
		CaddyHost:   caddyHostName(cfg.CaddyAdminURL),
	}
	set := map[string]bool{}
	for _, f := range adopt {
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
	if !doApply {
		return nil
	}

	if reason == "" {
		reason = "применение из командной строки"
	}
	res, err := ap.Apply(ctx, rc, apply.Options{Actor: "ssh", Reason: reason, Adopt: adopt})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, res.Message)
	if res.Snapshot != "" {
		fmt.Fprintf(out, "снимок: %s\n", res.Snapshot)
	}
	for d, st := range res.After {
		fmt.Fprintf(out, "  %-40s было: %-20s стало: %s\n", d, strings.TrimSpace(res.Before[d]), st)
	}
	if res.RolledBack {
		return fmt.Errorf("изменения откатились")
	}
	return nil
}

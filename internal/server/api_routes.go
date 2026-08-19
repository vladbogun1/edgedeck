package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/vladbogun1/edgedeck/internal/apply"
	"github.com/vladbogun1/edgedeck/internal/routes"
	"github.com/vladbogun1/edgedeck/internal/sites"
)

// Маршруты: чтение, план, применение, удаление, импорт, откат.
//
// Всякая запись идёт через apply.Apply — то есть через валидацию, снимок,
// контроль и возможный откат. Отдельного «просто записать файл» здесь нет
// специально: иначе рано или поздно кто-то (я) им воспользуется в обход проверок.

type routesResponse struct {
	Routes   []routeView  `json:"routes"`
	Manual   []manualView `json:"manual"`
	FilePath string       `json:"file_path"`
}

// routeView — маршрут плюс вычисленные поля для интерфейса.
//
// Поле называется TargetAddr, а не Target, специально: одноимённое поле
// перекрыло бы вложенное Route.Target, и в JSON цель вообще не попала бы —
// правка маршрута в интерфейсе молча теряла бы контейнер и порт.
type routeView struct {
	routes.Route
	ID         string `json:"id"`
	TargetAddr string `json:"target_addr"`
}

// manualView — файл, написанный руками. Панель его не трогает, но предлагает
// принять под управление, если разбор удался.
type manualView struct {
	File      string   `json:"file"`
	Addresses []string `json:"addresses"`
	Targets   []string `json:"targets"`
	CanImport bool     `json:"can_import"`
	WhyNot    string   `json:"why_not,omitempty"`
	IsDefault bool     `json:"is_default"`
}

func (a *app) handleRoutesList(w http.ResponseWriter, r *http.Request) {
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := routesResponse{FilePath: routes.Path(a.cfg.EdgeDir)}
	for _, rt := range cfg.Routes {
		resp.Routes = append(resp.Routes, routeView{Route: rt, ID: rt.ID(), TargetAddr: rt.Target.Addr()})
	}

	siteList, _ := sites.Load(a.cfg.SitesDir())
	for _, s := range siteList {
		if s.Managed {
			continue
		}
		mv := manualView{File: s.File, Addresses: s.Addresses, Targets: s.Targets, IsDefault: s.IsDefault}
		if s.IsDefault {
			mv.WhyNot = "служебный файл: обработчик по умолчанию, им управляет установщик"
		} else if body, err := os.ReadFile(filepath.Join(a.cfg.SitesDir(), s.File)); err == nil {
			if _, err := routes.Import(string(body)); err != nil {
				mv.WhyNot = err.Error()
			} else {
				mv.CanImport = true
			}
		}
		resp.Manual = append(resp.Manual, mv)
	}
	writeJSON(w, http.StatusOK, resp)
}

type saveRequest struct {
	// ID — какой маршрут заменяем. Пусто — создаём новый.
	ID    string       `json:"id"`
	Route routes.Route `json:"route"`
	// DryRun — только показать план, ничего не менять.
	DryRun bool `json:"dry_run"`
	// Adopt — файл, написанный руками, который согласны отдать под управление.
	Adopt string `json:"adopt"`
}

func (a *app) handleRouteSave(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не разобрал запрос"})
		return
	}
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rt := req.Route
	if err := rt.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := routes.Conflict(cfg.Routes, rt, req.ID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	if req.ID != "" {
		if i, ok := routes.FindByID(cfg.Routes, req.ID); ok {
			cfg.Routes[i] = rt
		} else {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "такого маршрута нет: " + req.ID})
			return
		}
	} else {
		cfg.Routes = append(cfg.Routes, rt)
	}

	var adopt []string
	if req.Adopt != "" {
		adopt = []string{req.Adopt}
	}

	if req.DryRun {
		writeJSON(w, http.StatusOK, a.applier.Plan(r.Context(), cfg, adoptSet(adopt)))
		return
	}

	action := "изменил маршрут " + rt.ID()
	if req.ID == "" {
		action = "добавил маршрут " + rt.ID()
	}
	a.applyAndRespond(w, r, cfg, adopt, action)
}

func (a *app) handleRouteDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		DryRun bool   `json:"dry_run"`
	}
	if err := decode(r, &req); err != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id маршрута"})
		return
	}
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	i, ok := routes.FindByID(cfg.Routes, req.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "такого маршрута нет"})
		return
	}
	cfg.Routes = append(cfg.Routes[:i:i], cfg.Routes[i+1:]...)

	if req.DryRun {
		writeJSON(w, http.StatusOK, a.applier.Plan(r.Context(), cfg, nil))
		return
	}
	a.applyAndRespond(w, r, cfg, nil, "удалил маршрут "+req.ID)
}

// handleRouteImport принимает файл, написанный руками, под управление панели:
// разбирает его в маршрут и применяет обычной транзакцией.
func (a *app) handleRouteImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File   string `json:"file"`
		DryRun bool   `json:"dry_run"`
	}
	if err := decode(r, &req); err != nil || req.File == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужно имя файла"})
		return
	}
	// Имя файла приходит от пользователя, поэтому берём только его basename:
	// «../../etc/passwd» здесь превратится в «passwd» и просто не найдётся.
	name := filepath.Base(req.File)
	if !strings.HasSuffix(name, ".caddy") || strings.HasPrefix(name, "_default") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "этот файл импортировать нельзя"})
		return
	}
	body, err := os.ReadFile(filepath.Join(a.cfg.SitesDir(), name))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден: " + name})
		return
	}
	rt, err := routes.Import(string(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не смог разобрать файл: " + err.Error()})
		return
	}
	cfg, err := routes.Load(a.cfg.EdgeDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := routes.Conflict(cfg.Routes, rt, ""); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	cfg.Routes = append(cfg.Routes, rt)

	if req.DryRun {
		writeJSON(w, http.StatusOK, a.applier.Plan(r.Context(), cfg, adoptSet([]string{name})))
		return
	}
	a.applyAndRespond(w, r, cfg, []string{name}, "принял под управление "+name)
}

func (a *app) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": a.applier.Snapshots()})
}

func (a *app) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужно имя снимка"})
		return
	}
	if err := a.applier.Restore(r.Context(), req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.state.Audit("rollback", clientIP(r), "вернул состояние из снимка "+req.Name, nil)
	a.public.invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "вернул файлы из снимка и применил"})
}

func (a *app) applyAndRespond(w http.ResponseWriter, r *http.Request, cfg routes.Config, adopt []string, action string) {
	actor := clientIP(r)
	res, err := a.applier.Apply(r.Context(), cfg, apply.Options{Actor: actor, Reason: action, Adopt: adopt})
	if err != nil {
		a.state.Audit("apply_failed", actor, action+" — не вышло: "+err.Error(), nil)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "changes": res.Changes})
		return
	}
	kind := "apply"
	if res.RolledBack {
		kind = "rollback_auto"
	}
	a.state.Audit(kind, actor, action+" — "+res.Message, map[string]any{"snapshot": res.Snapshot})
	a.public.invalidate()
	writeJSON(w, http.StatusOK, res)
}

func adoptSet(list []string) map[string]bool {
	out := map[string]bool{}
	for _, f := range list {
		out[f] = true
	}
	return out
}

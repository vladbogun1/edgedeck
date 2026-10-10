package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Флот: обзор всех серверов пользователя в одной панели (PLAN.md, Q-1).
//
// У каждого сервера своя панель, и управляет она только своим сервером — это
// не меняется. Флот добавляет чтение: панель отдаёт соседям сводку о себе
// (сайты, нагрузка, сертификаты, предупреждения), а любая панель собирает
// сводки соседей в общий обзор. Управлять соседом — только на его панели.
//
// Доверие — общий секрет EDGEDECK_FLEET_SECRET. Запрос подписан (время + путь),
// ответ тоже (тело), поэтому ходить можно и по IP с самоподписанным
// сертификатом, и по голому http (s2: :443 занят xray): подделать сводку нельзя.
// Читать её посторонний по http теоретически может — секретов в ней нет.
//
// Реестр — EDGEDECK_FLEET: через запятую «id|подпись|адрес для панелей|адрес для
// браузера», например
//
//	s1|прод|https://132.145.132.80|https://s1.bogun.site,
//	s2|vpn и игры|http://150.136.127.73|http://s2.bogun.site
//
// EDGEDECK_SERVER_ID — какой из них «я».

type fleetMember struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	API   string `json:"-"`
	Web   string `json:"url"`
	Self  bool   `json:"self"`
}

type fleetConfig struct {
	secret  []byte
	selfID  string
	members []fleetMember
}

func loadFleet() fleetConfig {
	fc := fleetConfig{
		secret: []byte(strings.TrimSpace(os.Getenv("EDGEDECK_FLEET_SECRET"))),
		selfID: strings.TrimSpace(os.Getenv("EDGEDECK_SERVER_ID")),
	}
	for _, item := range strings.Split(os.Getenv("EDGEDECK_FLEET"), ",") {
		parts := strings.Split(strings.TrimSpace(item), "|")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		m := fleetMember{ID: strings.TrimSpace(parts[0]), Label: strings.TrimSpace(parts[1]), API: strings.TrimRight(strings.TrimSpace(parts[2]), "/")}
		m.Web = m.API
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			m.Web = strings.TrimRight(strings.TrimSpace(parts[3]), "/")
		}
		m.Self = m.ID == fc.selfID
		fc.members = append(fc.members, m)
	}
	return fc
}

func (f fleetConfig) enabled() bool {
	return len(f.secret) >= 16 && len(f.members) > 0 && f.selfID != ""
}

func (f fleetConfig) mac(parts ...string) string {
	h := hmac.New(sha256.New, f.secret)
	h.Write([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

const fleetHeader = "X-EdgeDeck-Fleet"
const fleetSigHeader = "X-EdgeDeck-Fleet-Sig"

// checkRequest — запрос подписан нашим секретом и свежий (±2 минуты).
func (f fleetConfig) checkRequest(r *http.Request) bool {
	if !f.enabled() {
		return false
	}
	ts, sig, ok := strings.Cut(r.Header.Get(fleetHeader), ".")
	if !ok {
		return false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || abs(time.Now().Unix()-t) > 120 {
		return false
	}
	want := f.mac(ts, r.Method, r.URL.Path)
	return hmac.Equal([]byte(sig), []byte(want))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// fleetSummary — то, что сервер рассказывает о себе соседям.
type fleetSummary struct {
	ID       string       `json:"id"`
	Label    string       `json:"label"`
	Server   serverInfo   `json:"server"`
	Caddy    caddyStatus  `json:"caddy"`
	Sites    []publicSite `json:"sites"`
	Warnings []warning    `json:"warnings"`
	Certs    []fleetCert  `json:"certificates"`
	Seen     int          `json:"seen_unconfigured"` // свои домены без маршрута
	Foreign  int          `json:"foreign_zones"`
	Drift    bool         `json:"drift"`
	Time     time.Time    `json:"time"`
}

type fleetCert struct {
	Domains  []string `json:"domains"`
	DaysLeft int      `json:"days_left"`
}

func (a *app) buildFleetSummary(r *http.Request) fleetSummary {
	ov := a.buildOverview(r)
	s := fleetSummary{
		ID:       a.fleet.selfID,
		Server:   ov.Server,
		Caddy:    ov.Caddy,
		Sites:    a.publicSites(r.Context()),
		Warnings: ov.Warnings,
		Drift:    ov.Drift != nil,
		Time:     time.Now(),
	}
	for _, m := range a.fleet.members {
		if m.Self {
			s.Label = m.Label
		}
	}
	for _, c := range ov.Certs {
		if !c.SelfSign {
			s.Certs = append(s.Certs, fleetCert{Domains: c.Domains, DaysLeft: c.DaysLeft})
		}
	}
	zones := map[string]bool{}
	for _, d := range ov.SeenDomains {
		if d.Foreign {
			if d.PointsHere {
				zones[d.Zone] = true
			}
		} else {
			s.Seen++
		}
	}
	s.Foreign = len(zones)
	return s
}

// handleFleetSummary — для соседей: только подписанные запросы, ответ подписан.
func (a *app) handleFleetSummary(w http.ResponseWriter, r *http.Request) {
	if !a.fleet.checkRequest(r) {
		http.Error(w, "нет", http.StatusForbidden)
		return
	}
	body, _ := json.Marshal(a.buildFleetSummary(r))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(fleetSigHeader, a.fleet.mac(string(body)))
	_, _ = w.Write(body)
}

// fleetView — что видит интерфейс: свой сервер и соседи, с ошибкой у недоступных.
type fleetView struct {
	Member  fleetMember   `json:"member"`
	OK      bool          `json:"ok"`
	Error   string        `json:"error,omitempty"`
	Summary *fleetSummary `json:"summary,omitempty"`
}

var fleetClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{
		// Подлинность обеспечивает подпись ответа, а не сертификат: соседей
		// спрашиваем по IP, где сертификат самоподписанный.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	},
}

func (a *app) fetchFleetSummary(ctx context.Context, m fleetMember) (*fleetSummary, error) {
	const path = "/api/fleet/summary"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.API+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(fleetHeader, ts+"."+a.fleet.mac(ts, http.MethodGet, path))
	resp, err := fleetClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не отвечает: %s", shortNetErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ответил %d — разный секрет флота или старая версия панели", resp.StatusCode)
	}
	if !hmac.Equal([]byte(resp.Header.Get(fleetSigHeader)), []byte(a.fleet.mac(string(body)))) {
		return nil, fmt.Errorf("ответ без верной подписи — не доверяю")
	}
	var s fleetSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func shortNetErr(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

// handleFleet — общий обзор для вошедшего пользователя.
func (a *app) handleFleet(w http.ResponseWriter, r *http.Request) {
	if !a.fleet.enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "servers": []fleetView{}})
		return
	}
	out := make([]fleetView, len(a.fleet.members))
	var wg sync.WaitGroup
	for i, m := range a.fleet.members {
		out[i].Member = m
		if m.Self {
			s := a.buildFleetSummary(r)
			out[i].OK, out[i].Summary = true, &s
			continue
		}
		wg.Add(1)
		go func(i int, m fleetMember) {
			defer wg.Done()
			s, err := a.fetchFleetSummary(r.Context(), m)
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			out[i].OK, out[i].Summary = true, s
		}(i, m)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "self": a.fleet.selfID, "servers": out})
}

// handleFleetMembers — лёгкий список для переключателя серверов в шапке.
func (a *app) handleFleetMembers(w http.ResponseWriter, r *http.Request) {
	if !a.fleet.enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "members": []fleetMember{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "self": a.fleet.selfID, "members": a.fleet.members})
}

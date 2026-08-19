// Package hostports — какие порты слушает сам хост.
//
// Панель живёт в контейнере, поэтому /proc/net внутри неё показывает сетевое
// пространство контейнера, а не сервера. Читаем /host/proc/1/net/* — это
// пространство init'а хоста. Тот же приём использует node_exporter.
//
// Так находятся сервисы вне докера (pufferpanel, java-серверы, node_exporter),
// без которых картина портов неполная — DESIGN.md §2.5.
package hostports

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Port — одна слушающая точка на хосте.
type Port struct {
	Port     int    `json:"port"`
	Proto    string `json:"proto"` // tcp / udp
	BindIP   string `json:"bind_ip"`
	Process  string `json:"process"`
	PID      int    `json:"pid"`
	CmdLine  string `json:"cmdline"`
	IsDocker bool   `json:"is_docker"` // держит docker-proxy → это порт контейнера
	// LoopbackOnly — слушает только 127.0.0.1: из интернета не видно,
	// но и из контейнера не видно тоже (DESIGN.md §2.4).
	LoopbackOnly bool `json:"loopback_only"`

	// Guess — что это, скорее всего, за служба. Нужно потому, что имя процесса
	// панели не видно: чтобы его прочитать, контейнеру пришлось бы дать
	// pid: host и CAP_SYS_PTRACE, а таких прав панель себе не берёт
	// (DESIGN.md §2.26).
	Guess string `json:"guess"`

	// inodeCache — инод сокета, по которому ищем процесс-владелец.
	// Не экспортируется, значит в JSON не попадает.
	inodeCache uint64
}

type Reader struct {
	procDir string

	mu     sync.Mutex
	cache  []Port
	cached time.Time
	ttl    time.Duration
}

// wellKnown — минимальный справочник, чтобы список портов не был безымянным.
// Только то, что реально встречается на этих серверах.
var wellKnown = map[int]string{
	22: "SSH", 25: "почта", 53: "DNS", 67: "DHCP", 68: "DHCP", 111: "rpcbind",
	123: "NTP", 443: "HTTPS", 80: "HTTP", 1194: "OpenVPN", 2375: "Docker API",
	2376: "Docker API (TLS)", 3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis",
	8080: "веб-приложение", 9100: "node_exporter", 8429: "vmagent",
	25565: "Minecraft", 25575: "Minecraft RCON", 27017: "MongoDB",
}

func New(procDir string) *Reader {
	return &Reader{procDir: procDir, ttl: 15 * time.Second}
}

// Available говорит, смонтирован ли /proc хоста. Если нет — панель честно
// напишет, что порты вне докера показать не может, а не покажет пустой список.
func (r *Reader) Available() bool {
	_, err := os.Stat(filepath.Join(r.procDir, "1", "net", "tcp"))
	return err == nil
}

func (r *Reader) List() ([]Port, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.cached) < r.ttl && r.cache != nil {
		return r.cache, nil
	}
	if !r.Available() {
		return nil, fmt.Errorf("нет доступа к %s/1/net: смонтируйте /proc хоста только на чтение", r.procDir)
	}

	inodes := map[uint64]bool{}
	var ports []Port
	for _, f := range []struct{ file, proto string }{
		{"tcp", "tcp"}, {"tcp6", "tcp"}, {"udp", "udp"}, {"udp6", "udp"},
	} {
		got, err := r.parseNetFile(filepath.Join(r.procDir, "1", "net", f.file), f.proto)
		if err != nil {
			continue // tcp6 может отсутствовать: на s3 нет IPv6
		}
		for _, p := range got {
			inodes[p.inode] = true
			ports = append(ports, p.Port)
		}
	}

	// Кто владеет сокетом: раскрываем инод → pid → имя процесса.
	owners := r.socketOwners(inodes)
	for i := range ports {
		if o, ok := owners[ports[i].inodeCache]; ok {
			ports[i].PID = o.pid
			ports[i].Process = o.name
			ports[i].CmdLine = o.cmdline
			ports[i].IsDocker = strings.Contains(o.name, "docker-proxy") || strings.Contains(o.cmdline, "docker-proxy")
		}
	}

	// Дубли (IPv4+IPv6 одного слушателя) сворачиваем в одну строку.
	dedup := map[string]Port{}
	for _, p := range ports {
		key := fmt.Sprintf("%d/%s", p.Port, p.Proto)
		if old, ok := dedup[key]; ok {
			if old.LoopbackOnly && !p.LoopbackOnly {
				old.LoopbackOnly = false
				old.BindIP = p.BindIP
			}
			if old.Process == "" {
				old.Process, old.PID, old.CmdLine, old.IsDocker = p.Process, p.PID, p.CmdLine, p.IsDocker
			}
			dedup[key] = old
			continue
		}
		dedup[key] = p
	}
	out := make([]Port, 0, len(dedup))
	for _, p := range dedup {
		if p.Process == "" {
			p.Guess = wellKnown[p.Port]
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})

	r.cache, r.cached = out, time.Now()
	return out, nil
}

// ---- разбор /proc/<pid>/net/{tcp,udp} ----

type portWithInode struct {
	Port
	inode uint64
}

func (r *Reader) parseNetFile(path, proto string) ([]portWithInode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []portWithInode
	sc := bufio.NewScanner(f)
	sc.Scan() // заголовок
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		local, state, inodeStr := fields[1], fields[3], fields[9]

		// tcp: слушаем только 0A (LISTEN). udp: состояние 07, слушателем считаем всё.
		if proto == "tcp" && state != "0A" {
			continue
		}
		ip, port, err := parseHexAddr(local)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(inodeStr, 10, 64)
		if err != nil {
			continue
		}
		p := Port{
			Port:         port,
			Proto:        proto,
			BindIP:       ip,
			LoopbackOnly: strings.HasPrefix(ip, "127.") || ip == "::1",
		}
		p.inodeCache = inode
		out = append(out, portWithInode{Port: p, inode: inode})
	}
	return out, sc.Err()
}

// parseHexAddr разбирает «0100007F:1F90» (IPv4) и 32-символьный вид для IPv6.
func parseHexAddr(s string) (string, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("плохой адрес %q", s)
	}
	port64, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return "", 0, err
	}
	hex := parts[0]
	switch len(hex) {
	case 8:
		var b [4]byte
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
			if err != nil {
				return "", 0, err
			}
			b[3-i] = byte(v) // little-endian
		}
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]), int(port64), nil
	case 32:
		// Для наших целей достаточно различать «только локально» и «все адреса».
		if hex == strings.Repeat("0", 32) {
			return "::", int(port64), nil
		}
		if strings.HasSuffix(hex, "01000000") && strings.HasPrefix(hex, strings.Repeat("0", 24)) {
			return "::1", int(port64), nil
		}
		return "::", int(port64), nil
	default:
		return "", 0, fmt.Errorf("непонятная длина адреса %q", hex)
	}
}

// ---- инод сокета → процесс ----

type owner struct {
	pid     int
	name    string
	cmdline string
}

func (r *Reader) socketOwners(want map[uint64]bool) map[uint64]owner {
	out := map[uint64]owner{}
	if len(want) == 0 {
		return out
	}
	entries, err := os.ReadDir(r.procDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(r.procDir, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // чужой процесс без прав — просто пропускаем
		}
		var name, cmdline string
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64)
			if err != nil || !want[inode] {
				continue
			}
			if name == "" {
				name = readTrim(filepath.Join(r.procDir, e.Name(), "comm"))
				cmdline = readCmdline(filepath.Join(r.procDir, e.Name(), "cmdline"))
			}
			out[inode] = owner{pid: pid, name: name, cmdline: cmdline}
		}
	}
	return out
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readCmdline(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

package firewall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vladbogun1/edgedeck/internal/fwdump"
)

// Правила взяты с живых серверов — именно на них строятся выводы панели,
// и ошибка здесь означала бы, что панель уверенно говорит неправду.
var (
	// oracle-3: стоковый REJECT Oracle стоит ВЫШЕ перехода в ufw, но 22 разрешён
	// ещё выше — поэтому SSH работает, а всё остальное хостовое недостижимо.
	inputOracle3 = []string{
		"-P INPUT DROP",
		"-A INPUT -p tcp -m multiport --dports 22 -j f2b-sshd",
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT",
		"-A INPUT -p icmp -j ACCEPT",
		"-A INPUT -i lo -j ACCEPT",
		"-A INPUT -p tcp -m state --state NEW -m tcp --dport 22 -j ACCEPT",
		"-A INPUT -j REJECT --reject-with icmp-host-prohibited",
		"-A INPUT -j ufw-before-logging-input",
		"-A INPUT -j ufw-before-input",
	}
	// сервер 1: ufw-цепочки идут первыми, значит его правила действительно решают.
	inputServer1 = []string{
		"-P INPUT DROP",
		"-A INPUT -p tcp -m multiport --dports 22 -j f2b-sshd",
		"-A INPUT -j ufw-before-input",
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT",
		"-A INPUT -j REJECT --reject-with icmp-host-prohibited",
	}
	ufwRules = []string{
		"-A ufw-user-input -p tcp -m tcp --dport 8080 -j ACCEPT",
		"-A ufw-user-input -p tcp -m tcp --dport 9001 -s 132.145.132.80/32 -j ACCEPT",
	}
	dockerUser9001 = []string{
		"-N DOCKER-USER",
		"-A DOCKER-USER -s 132.145.132.80/32 -p tcp -m tcp --dport 9001 -j RETURN",
		"-A DOCKER-USER -p tcp -m tcp --dport 9001 -j DROP",
	}
)

func reader(t *testing.T, d fwdump.Dump) *Reader {
	t.Helper()
	d.Time = time.Now()
	path := filepath.Join(t.TempDir(), "firewall.json")
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return New(path)
}

func TestDockerPorts(t *testing.T) {
	r := reader(t, fwdump.Dump{DockerUser: dockerUser9001, Input: inputOracle3})

	// Порт 80 опубликован докером, правил для него нет → виден всем.
	if v := r.Judge(80, "tcp", true, false); v.Level != "open" {
		t.Errorf("80/tcp: ожидал open, получил %s (%s)", v.Level, v.Summary)
	}
	// 9001 ограничен парой правил RETURN+DROP → доступен только с адреса.
	v := r.Judge(9001, "tcp", true, false)
	if v.Level != "limited" || len(v.Allowed) != 1 || v.Allowed[0] != "132.145.132.80/32" {
		t.Errorf("9001/tcp: ожидал limited для 132.145.132.80/32, получил %s %v", v.Level, v.Allowed)
	}
	// Порт на 127.0.0.1 закрыт снаружи независимо от правил.
	if v := r.Judge(8087, "tcp", true, true); v.Level != "closed" {
		t.Errorf("loopback: ожидал closed, получил %s", v.Level)
	}
}

// Главный случай: на oracle-3 SSH доступен, хотя ufw там не работает вовсе.
// Прошлая версия отвечала «недостижим» для всех хостовых портов — и врала.
func TestHostPortsOracle3(t *testing.T) {
	r := reader(t, fwdump.Dump{Input: inputOracle3, UFW: ufwRules, UFWWorks: false})

	if v := r.Judge(22, "tcp", false, false); v.Level != "open" {
		t.Errorf("22/tcp на oracle-3: ожидал open (Oracle разрешает его выше REJECT), получил %s (%s)", v.Level, v.Summary)
	}
	for _, port := range []int{111, 8080} {
		v := r.Judge(port, "tcp", false, false)
		if v.Level != "closed" {
			t.Errorf("%d/tcp на oracle-3: ожидал closed, получил %s (%s)", port, v.Level, v.Summary)
		}
	}
	// Даже при наличии правила ufw порт остаётся недостижимым — в этом вся соль.
	if v := r.Judge(8080, "tcp", false, false); !contains(v.Detail, "до правил ufw") && !contains(v.Summary, "недостижим") {
		t.Errorf("8080/tcp: ожидал объяснение про недостижимость ufw, получил %q / %q", v.Summary, v.Detail)
	}
}

func TestHostPortsServer1(t *testing.T) {
	r := reader(t, fwdump.Dump{Input: inputServer1, UFW: ufwRules, UFWWorks: true})

	if v := r.Judge(8080, "tcp", false, false); v.Level != "open" {
		t.Errorf("8080/tcp на сервере 1: ожидал open, получил %s (%s)", v.Level, v.Summary)
	}
	if v := r.Judge(9001, "tcp", false, false); v.Level != "limited" {
		t.Errorf("9001/tcp на сервере 1: ожидал limited, получил %s (%s)", v.Level, v.Summary)
	}
	if v := r.Judge(5432, "tcp", false, false); v.Level != "closed" {
		t.Errorf("5432/tcp без правила ufw: ожидал closed, получил %s", v.Level)
	}
}

// Устаревшему снимку верить нельзя: лучше сказать «не знаю», чем соврать.
func TestStaleDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.json")
	old := fwdump.Dump{Time: time.Now().Add(-time.Hour), Input: inputServer1}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if v := New(path).Judge(80, "tcp", true, false); v.Level != "unknown" {
		t.Errorf("устаревший снимок: ожидал unknown, получил %s", v.Level)
	}
}

func TestRuleMatchesPort(t *testing.T) {
	cases := []struct {
		rule  string
		port  int
		proto string
		want  bool
	}{
		{"-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT", 22, "tcp", true},
		{"-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT", 22, "udp", false},
		{"-A INPUT -p tcp -m multiport --dports 22,80 -j ACCEPT", 80, "tcp", true},
		{"-A INPUT -i lo -j ACCEPT", 80, "tcp", false}, // общее правило — не про порт
	}
	for _, c := range cases {
		if got := ruleMatchesPort(c.rule, c.port, c.proto); got != c.want {
			t.Errorf("ruleMatchesPort(%q, %d, %s) = %v, ожидалось %v", c.rule, c.port, c.proto, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

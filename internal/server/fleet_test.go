package server

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestFleetSignature(t *testing.T) {
	f := fleetConfig{secret: []byte("0123456789abcdef0123"), selfID: "s1", members: []fleetMember{{ID: "s1", Self: true}}}
	sign := func(ts int64, path string) string {
		s := strconv.FormatInt(ts, 10)
		return s + "." + f.mac(s, "GET", path)
	}
	r := httptest.NewRequest("GET", "/api/fleet/summary", nil)
	r.Header.Set(fleetHeader, sign(time.Now().Unix(), "/api/fleet/summary"))
	if !f.checkRequest(r) {
		t.Fatal("свежая верная подпись отвергнута")
	}
	r.Header.Set(fleetHeader, sign(time.Now().Unix()-600, "/api/fleet/summary"))
	if f.checkRequest(r) {
		t.Fatal("старая подпись принята")
	}
	r.Header.Set(fleetHeader, sign(time.Now().Unix(), "/api/other"))
	if f.checkRequest(r) {
		t.Fatal("подпись другого пути принята")
	}
	other := fleetConfig{secret: []byte("другой-секрет-12345"), selfID: "s1", members: f.members}
	r.Header.Set(fleetHeader, sign(time.Now().Unix(), "/api/fleet/summary"))
	if other.checkRequest(r) {
		t.Fatal("подпись чужим секретом принята")
	}
}

func TestLoadFleet(t *testing.T) {
	t.Setenv("EDGEDECK_FLEET_SECRET", "0123456789abcdef")
	t.Setenv("EDGEDECK_SERVER_ID", "s2")
	t.Setenv("EDGEDECK_FLEET", "s1|прод|https://1.1.1.1/|https://s1.example.com, s2|vpn|http://2.2.2.2")
	f := loadFleet()
	if !f.enabled() || len(f.members) != 2 {
		t.Fatalf("реестр не разобран: %+v", f.members)
	}
	if f.members[0].API != "https://1.1.1.1" || f.members[0].Web != "https://s1.example.com" || f.members[0].Self {
		t.Errorf("s1: %+v", f.members[0])
	}
	if f.members[1].Web != "http://2.2.2.2" || !f.members[1].Self {
		t.Errorf("s2: %+v", f.members[1])
	}
}

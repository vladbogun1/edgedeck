package server

import "testing"

func TestOTP(t *testing.T) {
	s := newOTPStore()
	id, code, err := s.issue("1.1.1.1")
	if err != nil || len(code) != otpDigits {
		t.Fatalf("issue: %v %q", err, code)
	}
	if _, _, err := s.issue("1.1.1.1"); err == nil {
		t.Error("второй код с того же адреса сразу — должен быть отказ")
	}
	if s.check("чужой", code) {
		t.Error("код без своего вызова принят")
	}
	if !s.check(id, code) {
		t.Error("верный код не принят")
	}
	if s.check(id, code) {
		t.Error("код сработал дважды")
	}

	id, code, _ = s.issue("2.2.2.2")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < otpTries; i++ {
		s.check(id, wrong)
	}
	if s.check(id, code) {
		t.Error("после исчерпанных попыток вызов должен сгореть")
	}
}

func TestOTPLiveLimit(t *testing.T) {
	s := newOTPStore()
	first, _, _ := s.issue("10.0.0.1")
	for i := 2; i <= otpLive+1; i++ {
		if _, _, err := s.issue("10.0.0." + string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.challenges) != otpLive {
		t.Errorf("живых вызовов %d, ждали %d", len(s.challenges), otpLive)
	}
	if _, ok := s.challenges[first]; ok {
		t.Error("самый старый вызов должен уступить место")
	}
}

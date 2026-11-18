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
	evilSubject := "subject\r\nX-Evil-Subject: yes"
	if err := m.sendMail(evil, evilSubject, "body"); err != nil {
		t.Fatal(err)
	}
	s := string(captured)
	// After the CRLF-stripping fix the attacker's newlines are removed, so no
	// extra header LINE can be injected through the recipient or the subject.
	// The payload text may survive inline on the To:/Subject: line (harmless);
	// what must never appear is a new CRLF-delimited header.
	if strings.Contains(s, "\r\nBcc:") || strings.Contains(s, "\r\nX-Injected:") || strings.Contains(s, "\r\nX-Evil-Subject:") {
		t.Fatalf("INJECTION VULNERABLE:\n%s", s)
	}
	t.Logf("mitigation confirmed:\n%s", s)
}

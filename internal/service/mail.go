// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
)

type Mailer interface {
	SendConfirmationEmail(to, token string) error
}

type MailService struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	BaseURL  string
	// Internal field for testing smtp.SendMail
	sendMailFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// BUG-L04 FIX: NewMailService now accepts an optional cfgSMTPPort parameter
// to allow the SMTP port to be sourced from the application config struct
// (which reads from environment variables via cleanenv). Previously, the port
// was only read directly from os.Getenv("SMTP_PORT"), bypassing the config
// struct and its default value. If no port is provided via config or env, it
// defaults to "587" (standard SMTP submission port) instead of empty string.
func NewMailService(cfgSMTPPort ...string) *MailService {
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	port := os.Getenv("SMTP_PORT")
	if port == "" && len(cfgSMTPPort) > 0 {
		port = cfgSMTPPort[0]
	}
	if port == "" {
		port = "587" // Standard SMTP submission port
	}

	return &MailService{
		Host:         os.Getenv("SMTP_HOST"),
		Port:         port,
		Username:     os.Getenv("SMTP_USER"),
		Password:     os.Getenv("SMTP_PASS"),
		From:         os.Getenv("SMTP_FROM"),
		BaseURL:      baseURL,
		sendMailFunc: smtp.SendMail,
	}
}

func (s *MailService) SendConfirmationEmail(to, token string) error {
	subject := "Verify your Pokget account"
	confirmURL := fmt.Sprintf("%s/auth/confirm?token=%s", s.BaseURL, token)

	data := map[string]string{
		"ConfirmURL": confirmURL,
	}

	tmpl, err := template.New("confirm").Parse(confirmEmailTemplate)
	if err != nil {
		return err
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return err
	}

	// Log the confirmation URL for easier development/testing if SMTP fails
	slog.Info("Sending confirmation email", "to", to)

	return s.sendMail(to, subject, body.String())
}

func (s *MailService) sendMail(to, subject, body string) error {
	// Ensure we don't send credentials over unencrypted connections
	// PlainAuth will refuse to send credentials unless the connection is TLS
	// or the server supports STARTTLS (Go's smtp package handles this for port 587)
	// For explicit safety, verify the host/port combination
	if s.Port != "587" {
		return fmt.Errorf("mail: only STARTTLS-capable port 587 is supported for secure mail transmission (got port %s)", s.Port)
	}

	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	// Go's smtp.SendMail automatically upgrades to TLS via STARTTLS on port 587
	// PlainAuth will refuse to send credentials if TLS is not established
	msg := []byte(fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n"+
		"\r\n"+
		"%s\r\n", s.From, to, subject, body))

	addr := fmt.Sprintf("%s:%s", s.Host, s.Port)
	return s.sendMailFunc(addr, auth, s.From, []string{to}, msg)
}

const confirmEmailTemplate = `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: 'Inter', -apple-system, sans-serif; background-color: #0a0a0a; color: #ffffff; padding: 40px; }
        .card { background: rgba(255, 255, 255, 0.05); border: 1px solid rgba(255, 255, 255, 0.1); border-radius: 24px; padding: 32px; max-width: 500px; margin: 0 auto; text-align: center; }
        .logo { font-size: 24px; font-weight: 800; background: linear-gradient(to right, #a855f7, #3b82f6); -webkit-background-clip: text; -webkit-text-fill-color: transparent; margin-bottom: 24px; }
        .btn { display: inline-block; background: #9333ea; color: white; padding: 16px 32px; border-radius: 12px; text-decoration: none; font-weight: bold; margin-top: 24px; box-shadow: 0 10px 15px -3px rgba(147, 51, 234, 0.3); }
        .footer { margin-top: 32px; color: rgba(255, 255, 255, 0.4); font-size: 12px; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">POKGET</div>
        <h1>Welcome to the collection!</h1>
        <p>You're one step away from tracking your TCG portfolio like a pro. Click the button below to verify your email.</p>
        <a href="{{.ConfirmURL}}" class="btn">Verify Account</a>
        <div class="footer">
            If you didn't create an account, you can safely ignore this email.
        </div>
    </div>
</body>
</html>
`

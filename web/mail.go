// Sending the login link. The server only ever emails one thing — "click
// here to log in" — so the Mailer interface is exactly that narrow, and local
// development doesn't need a provider at all: the log mailer prints the link
// to stdout, where `make run` shows it.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Mailer delivers a login link to an address.
type Mailer interface {
	SendLogin(to, url string) error
}

// LogMailer is the development mailer: the "email" is a log line.
type LogMailer struct{}

func (LogMailer) SendLogin(to, url string) error {
	log.Printf("login link for %s: %s", to, url)
	return nil
}

// ResendMailer sends through resend.com's transactional API: one HTTPS POST,
// authenticated by the API key.
type ResendMailer struct {
	Key  string // API key (re_…)
	From string // sender, e.g. "study <study@fftp.io>" — a domain verified with Resend
}

func (m ResendMailer) SendLogin(to, url string) error {
	body, err := json.Marshal(map[string]any{
		"from":    m.From,
		"to":      []string{to},
		"subject": "Your study login link",
		"text": "Click to log in:\n\n" + url + "\n\n" +
			"The link works once and expires in 15 minutes. " +
			"If you didn't ask for it, ignore this email.",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.Key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending login email: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("sending login email: %s: %s", resp.Status, detail)
	}
	return nil
}

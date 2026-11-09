// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"bufio"
	"bytes"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/auth"
)

func saveWebhook(t *testing.T, h *harness) alerts.Channel {
	t.Helper()
	return saveChannel(t, h, alerts.Channel{Name: "Ops", Type: alerts.ChannelWebhook, Enabled: true,
		Config: alerts.ChannelConfig{URL: h.receiver.url + "/hook"}})
}

func saveChannel(t *testing.T, h *harness, channel alerts.Channel) alerts.Channel {
	t.Helper()
	saved, err := h.service.SaveChannel(t.Context(), h.admin, auth.RequestInfo{}, channel)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestSaveChannelValidation(t *testing.T) {
	email := alerts.ChannelConfig{SMTPHost: "mail.example.com", From: "sentinel@example.com", To: []string{"ops@example.com"}}
	cases := []struct {
		name    string
		channel alerts.Channel
		field   string
	}{
		{"refuses a channel with no name", alerts.Channel{Type: alerts.ChannelWebhook, Config: alerts.ChannelConfig{URL: "https://example.com"}}, "name"},
		{"refuses an unknown type", alerts.Channel{Name: "x", Type: "pager"}, "type"},
		{"refuses a webhook that isn't a web address", alerts.Channel{Name: "x", Type: alerts.ChannelWebhook, Config: alerts.ChannelConfig{URL: "ftp://example.com"}}, "url"},
		{"refuses a Discord webhook with no address", alerts.Channel{Name: "x", Type: alerts.ChannelDiscord}, "url"},
		{"refuses an ntfy topic with a slash", alerts.Channel{Name: "x", Type: alerts.ChannelNtfy, Config: alerts.ChannelConfig{Topic: "a/b"}}, "topic"},
		{"refuses an ntfy server that isn't a web address", alerts.Channel{Name: "x", Type: alerts.ChannelNtfy, Config: alerts.ChannelConfig{Topic: "a", Server: "ntfy"}}, "server"},
		{"refuses email with no mail server", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: alerts.ChannelConfig{From: email.From, To: email.To}}, "smtpHost"},
		{"refuses an unknown email security", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: func() alerts.ChannelConfig {
			c := email
			c.SMTPSecurity = "ssl3"
			return c
		}()}, "smtpSecurity"},
		{"refuses a port out of range", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: func() alerts.ChannelConfig {
			c := email
			c.SMTPPort = 70000
			return c
		}()}, "smtpPort"},
		{"refuses email from no one", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: func() alerts.ChannelConfig {
			c := email
			c.From = "sentinel"
			return c
		}()}, "from"},
		{"refuses email to no one", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: func() alerts.ChannelConfig {
			c := email
			c.To = nil
			return c
		}()}, "to"},
		{"refuses email to something that isn't an address", alerts.Channel{Name: "x", Type: alerts.ChannelEmail, Config: func() alerts.ChannelConfig {
			c := email
			c.To = []string{"ops"}
			return c
		}()}, "to"},
	}
	h := newHarness(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.service.SaveChannel(t.Context(), h.admin, auth.RequestInfo{}, tc.channel)
			var invalid *alerts.InvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Errorf("got %v, want a problem with %s", err, tc.field)
			}
		})
	}
}

func TestChannels(t *testing.T) {
	t.Run("fills in defaults and describes each channel without its secrets", func(t *testing.T) {
		h := newHarness(t)
		saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
			SMTPHost: "mail.example.com", From: "sentinel@example.com", To: []string{"ops@example.com", "me@example.com"},
		}})
		saveChannel(t, h, alerts.Channel{Name: "Phone", Type: alerts.ChannelNtfy, Enabled: true, Config: alerts.ChannelConfig{Topic: "homelab"}})
		saveChannel(t, h, alerts.Channel{Name: "chat", Type: alerts.ChannelDiscord, Config: alerts.ChannelConfig{URL: "https://discord.com/api/webhooks/1/secret"}})
		channels, err := h.service.Channels(t.Context())
		if err != nil || len(channels) != 3 {
			t.Fatalf("got %+v, %v", channels, err)
		}
		summaries := []string{}
		for _, channel := range channels {
			summaries = append(summaries, channel.Name+": "+channel.Summary())
		}
		want := []string{"chat: posts to discord.com", "Mail: mail.example.com to ops@example.com, me@example.com", "Phone: topic homelab on ntfy.sh"}
		if !equalStrings(summaries, want) {
			t.Errorf("got %q", summaries)
		}
		if mail := channels[1].Config; mail.SMTPPort != 587 || mail.SMTPSecurity != "starttls" {
			t.Errorf("email defaults: %+v", mail)
		}
	})

	t.Run("keeps stored secrets when a change leaves them out", func(t *testing.T) {
		h := newHarness(t)
		saved := saveChannel(t, h, alerts.Channel{Name: "Phone", Type: alerts.ChannelNtfy, Enabled: true,
			Config: alerts.ChannelConfig{Server: h.receiver.url, Topic: "homelab", Token: "tk_secret"}})
		saved.Config.Token, saved.Enabled = "", false
		if _, err := h.service.SaveChannel(t.Context(), h.admin, auth.RequestInfo{}, saved); err != nil {
			t.Fatal(err)
		}
		channels, err := h.service.Channels(t.Context())
		if err != nil || len(channels) != 1 || channels[0].Config.Token != "tk_secret" || channels[0].Enabled {
			t.Errorf("got %+v, %v", channels, err)
		}
	})

	t.Run("refuses a second channel with the same name, and changes to a missing one", func(t *testing.T) {
		h := newHarness(t)
		saveWebhook(t, h)
		if _, err := h.service.SaveChannel(t.Context(), h.admin, auth.RequestInfo{}, alerts.Channel{
			Name: "Ops", Type: alerts.ChannelWebhook, Config: alerts.ChannelConfig{URL: "https://example.com"},
		}); !errors.Is(err, alerts.ErrExists) {
			t.Errorf("got %v", err)
		}
		if _, err := h.service.SaveChannel(t.Context(), h.admin, auth.RequestInfo{}, alerts.Channel{ID: 99, Name: "x"}); !errors.Is(err, alerts.ErrNotFound) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("deletes a channel, taking it off the rules that used it", func(t *testing.T) {
		h := newHarness(t)
		channel := saveWebhook(t, h)
		rule := cpuRule("above", 90)
		rule.ChannelIDs = []int64{channel.ID}
		if _, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{}, rule); err != nil {
			t.Fatal(err)
		}
		if err := h.service.DeleteChannel(t.Context(), h.admin, auth.RequestInfo{}, channel.ID); err != nil {
			t.Fatal(err)
		}
		if err := h.service.DeleteChannel(t.Context(), h.admin, auth.RequestInfo{}, channel.ID); !errors.Is(err, alerts.ErrNotFound) {
			t.Errorf("deleting it again: %v", err)
		}
		rules, _ := h.service.Rules(t.Context())
		for _, r := range rules {
			if len(r.ChannelIDs) != 0 {
				t.Errorf("rule %s still uses %v", r.Name, r.ChannelIDs)
			}
		}
	})

	t.Run("refuses anyone who can't manage alerts", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.SaveChannel(t.Context(), h.viewer, auth.RequestInfo{}, alerts.Channel{}); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("save: %v", err)
		}
		if err := h.service.DeleteChannel(t.Context(), h.viewer, auth.RequestInfo{}, 1); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("delete: %v", err)
		}
		if err := h.service.TestChannel(t.Context(), h.viewer, 1); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("test: %v", err)
		}
	})
}

func TestSEC42_NotificationChannelSecretsAreStoredEncrypted(t *testing.T) {
	h := newHarness(t)
	password := "hunter2-smtp" //nolint:gosec // G101: a test password, checked to be stored encrypted
	saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
		SMTPHost: "mail.example.com", SMTPUsername: "sentinel", SMTPPassword: password,
		From: "sentinel@example.com", To: []string{"ops@example.com"},
	}})
	saveChannel(t, h, alerts.Channel{Name: "Hook", Type: alerts.ChannelWebhook, Enabled: true,
		Config: alerts.ChannelConfig{URL: "https://hooks.example.com/T0KEN-in-url"}})
	var stored []byte
	err := h.database.Read(t.Context(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT config_encrypted FROM notification_channels`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var sealed []byte
			if err := rows.Scan(&sealed); err != nil {
				return err
			}
			stored = append(stored, sealed...)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2-smtp", "T0KEN-in-url", "mail.example.com"} {
		if bytes.Contains(stored, []byte(secret)) {
			t.Errorf("%q is stored in the clear", secret)
		}
	}

	t.Run("binds the sealed settings to the channel's type", func(t *testing.T) {
		h.exec(t, `UPDATE notification_channels SET type = 'discord' WHERE name = 'Hook'`)
		if _, err := h.service.Channels(t.Context()); err == nil {
			t.Error("settings moved to another type of channel were decrypted")
		}
	})
}

func TestTestChannel(t *testing.T) {
	t.Run("posts a webhook notification as JSON and records it was sent", func(t *testing.T) {
		h := newHarness(t)
		channel := saveWebhook(t, h)
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err != nil {
			t.Fatal(err)
		}
		got := h.receiver.take()
		if len(got) != 1 || got[0].Path != "/hook" || got[0].Headers.Get("Content-Type") != "application/json" {
			t.Fatalf("got %+v", got)
		}
		if body := got[0].JSON(t); body["event"] != "test" || body["machine"] != "Sentinel" {
			t.Errorf("got %v", body)
		}
		channels, _ := h.service.Channels(t.Context())
		if channels[0].LastSentAt == nil || *channels[0].LastSentAt != h.clock.Now().UnixMilli() || channels[0].LastError != "" {
			t.Errorf("got %+v", channels[0])
		}
	})

	t.Run("posts a Discord message with the notification as its content", func(t *testing.T) {
		h := newHarness(t)
		channel := saveChannel(t, h, alerts.Channel{Name: "Chat", Type: alerts.ChannelDiscord, Enabled: true,
			Config: alerts.ChannelConfig{URL: h.receiver.url + "/api/webhooks/1/abc"}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err != nil {
			t.Fatal(err)
		}
		got := h.receiver.take()
		if len(got) != 1 || got[0].JSON(t)["content"] != "Sentinel test notification" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("publishes to an ntfy topic with a title, priority, tag and token", func(t *testing.T) {
		h := newHarness(t)
		channel := saveChannel(t, h, alerts.Channel{Name: "Phone", Type: alerts.ChannelNtfy, Enabled: true,
			Config: alerts.ChannelConfig{Server: h.receiver.url + "/", Topic: "homelab", Token: "tk_1"}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err != nil {
			t.Fatal(err)
		}
		got := h.receiver.take()
		if len(got) != 1 || got[0].Path != "/homelab" || got[0].Body != "This is a test notification from Sentinel." ||
			got[0].Headers.Get("Title") != "Sentinel test notification" || got[0].Headers.Get("Priority") != "4" ||
			got[0].Headers.Get("Authorization") != "Bearer tk_1" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("reports a refused notification and records why", func(t *testing.T) {
		h := newHarness(t)
		channel := saveWebhook(t, h)
		h.receiver.refuse(http.StatusForbidden)
		err := h.service.TestChannel(t.Context(), h.admin, channel.ID)
		var failed *alerts.DeliveryError
		if !errors.As(err, &failed) || !strings.Contains(err.Error(), "403") {
			t.Fatalf("got %v", err)
		}
		channels, _ := h.service.Channels(t.Context())
		if !strings.Contains(channels[0].LastError, "403") {
			t.Errorf("got %+v", channels[0])
		}
	})

	t.Run("reports a channel that doesn't exist", func(t *testing.T) {
		h := newHarness(t)
		if err := h.service.TestChannel(t.Context(), h.admin, 42); !errors.Is(err, alerts.ErrNotFound) {
			t.Errorf("got %v", err)
		}
	})
}

func TestEmail(t *testing.T) {
	t.Run("sends a plain-text email, signing in when a username is set", func(t *testing.T) {
		h := newHarness(t)
		mail := newMailServer(t, false)
		channel := saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
			SMTPHost: "127.0.0.1", SMTPPort: mail.port, SMTPSecurity: "none", SMTPUsername: "sentinel", SMTPPassword: "pw",
			From: "sentinel@example.com", To: []string{"ops@example.com", "me@example.com"},
		}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err != nil {
			t.Fatal(err)
		}
		got := mail.transcript()
		for _, want := range []string{"AUTH PLAIN", "MAIL FROM:<sentinel@example.com>", "RCPT TO:<ops@example.com>", "RCPT TO:<me@example.com>",
			"Subject: [Sentinel] Sentinel test notification", "This is a test notification from Sentinel.", "QUIT"} {
			if !strings.Contains(got, want) {
				t.Errorf("the conversation has no %q:\n%s", want, got)
			}
		}
	})

	t.Run("refuses to send without encryption when STARTTLS is required but not offered", func(t *testing.T) {
		h := newHarness(t)
		mail := newMailServer(t, false)
		channel := saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
			SMTPHost: "127.0.0.1", SMTPPort: mail.port, From: "sentinel@example.com", To: []string{"ops@example.com"},
		}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err == nil || !strings.Contains(err.Error(), "TLS") {
			t.Errorf("got %v", err)
		}
		if strings.Contains(mail.transcript(), "MAIL FROM") {
			t.Error("the email was sent in the clear")
		}
	})

	t.Run("reports a recipient the server refuses", func(t *testing.T) {
		h := newHarness(t)
		mail := newMailServer(t, true)
		channel := saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
			SMTPHost: "127.0.0.1", SMTPPort: mail.port, SMTPSecurity: "none", From: "sentinel@example.com", To: []string{"ops@example.com"},
		}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err == nil || !strings.Contains(err.Error(), "ops@example.com") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("reports a mail server it can't reach", func(t *testing.T) {
		h := newHarness(t)
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		channel := saveChannel(t, h, alerts.Channel{Name: "Mail", Type: alerts.ChannelEmail, Enabled: true, Config: alerts.ChannelConfig{
			SMTPHost: "127.0.0.1", SMTPPort: port, SMTPSecurity: "tls", From: "sentinel@example.com", To: []string{"ops@example.com"},
		}})
		if err := h.service.TestChannel(t.Context(), h.admin, channel.ID); err == nil || !strings.Contains(err.Error(), "reach the mail server") {
			t.Errorf("got %v", err)
		}
	})
}

// mailServer is a minimal SMTP server that records the conversation.
type mailServer struct {
	port  int
	mutex sync.Mutex
	lines []string
	// refuseRecipients answers every RCPT with 550.
	refuseRecipients bool
}

func newMailServer(t *testing.T, refuseRecipients bool) *mailServer {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := &mailServer{port: listener.Addr().(*net.TCPAddr).Port, refuseRecipients: refuseRecipients}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(conn)
		}
	}()
	return server
}

func (m *mailServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 mail.test ESMTP")
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		m.mutex.Lock()
		m.lines = append(m.lines, line)
		m.mutex.Unlock()
		if inData {
			if line == "." {
				inData = false
				reply("250 queued")
			}
			continue
		}
		command := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch {
		case command == "EHLO":
			reply("250-mail.test")
			reply("250 AUTH PLAIN")
		case command == "AUTH":
			reply("235 accepted")
		case command == "RCPT" && m.refuseRecipients:
			reply("550 no such user")
		case command == "DATA":
			inData = true
			reply("354 go ahead")
		case command == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (m *mailServer) transcript() string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return strings.Join(m.lines, "\n")
}

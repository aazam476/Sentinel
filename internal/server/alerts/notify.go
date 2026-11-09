// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Notification events.
const (
	EventFiring   = "firing"
	EventResolved = "resolved"
	EventTest     = "test"
)

// sendTimeout bounds one notification; a slow channel can't hold up others.
const sendTimeout = 15 * time.Second

// Notification is what a channel is told about an alert.
type Notification struct {
	Event    string    `json:"event"`
	AlertID  int64     `json:"alertId"`
	Machine  string    `json:"machine"`
	Rule     string    `json:"rule"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	At       time.Time `json:"time"`
}

// title is the notification's one-line summary.
func (n Notification) title() string {
	switch n.Event {
	case EventResolved:
		return "Resolved on " + n.Machine + ": " + n.Message
	case EventTest:
		return "Sentinel test notification"
	}
	return strings.ToUpper(n.Severity[:1]) + n.Severity[1:] + " on " + n.Machine + ": " + n.Message
}

// deliver sends a notification through a channel, and records how it went.
func (s *Service) deliver(ctx context.Context, channel Channel, n Notification) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	var err error
	switch channel.Type {
	case ChannelEmail:
		err = s.sendEmail(ctx, channel.Config, n)
	case ChannelWebhook:
		err = s.postJSON(ctx, channel.Config.URL, n, nil)
	case ChannelDiscord:
		err = s.postJSON(ctx, channel.Config.URL, map[string]any{"username": "Sentinel", "content": n.title()}, nil)
	case ChannelNtfy:
		err = s.sendNtfy(ctx, channel.Config, n)
	default:
		err = fmt.Errorf("unknown channel type %q", channel.Type)
	}
	s.recordDelivery(context.WithoutCancel(ctx), channel.ID, err)
	return err
}

func (s *Service) recordDelivery(ctx context.Context, channelID int64, sendErr error) {
	problem := ""
	if sendErr != nil {
		problem = sendErr.Error()
		s.logger.Warn("a notification couldn't be sent", "channel", channelID, "error", sendErr)
	}
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE notification_channels SET last_sent_at = ?, last_error = ? WHERE id = ?`,
			s.clock.Now().UnixMilli(), problem, channelID)
		return err
	})
	if err != nil {
		s.logger.Error("could not record a notification", "channel", channelID, "error", err)
	}
}

// postJSON posts body as JSON and expects a 2xx answer.
func (s *Service) postJSON(ctx context.Context, address string, body any, headers map[string]string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode the notification: %w", err)
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	return s.post(ctx, address, encoded, headers)
}

func (s *Service) post(ctx context.Context, address string, body []byte, headers map[string]string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("prepare the notification: %w", err)
	}
	request.Header.Set("User-Agent", "Sentinel")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send the notification: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10)) // let the connection be reused
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("the notification was refused: %s", response.Status)
	}
	return nil
}

// sendNtfy publishes to an ntfy topic, with the alert's title, a priority
// that follows its severity, and a tag ntfy shows as an emoji.
func (s *Service) sendNtfy(ctx context.Context, config ChannelConfig, n Notification) error {
	priority, tag := "4", "warning"
	switch {
	case n.Event == EventResolved:
		priority, tag = "3", "white_check_mark"
	case n.Severity == Critical:
		priority, tag = "5", "rotating_light"
	}
	headers := map[string]string{"Title": n.title(), "Priority": priority, "Tags": tag}
	if config.Token != "" {
		headers["Authorization"] = "Bearer " + config.Token
	}
	return s.post(ctx, config.Server+"/"+config.Topic, []byte(n.Message), headers)
}

// sendEmail sends a plain-text email over SMTP.
func (s *Service) sendEmail(ctx context.Context, config ChannelConfig, n Notification) error {
	client, err := s.dialSMTP(ctx, config)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if config.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", config.SMTPUsername, config.SMTPPassword, config.SMTPHost)); err != nil {
			return fmt.Errorf("sign in to the mail server: %w", err)
		}
	}
	if err := client.Mail(config.From); err != nil {
		return fmt.Errorf("send the email: %w", err)
	}
	for _, to := range config.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("send the email to %s: %w", to, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("send the email: %w", err)
	}
	if _, err := writer.Write(emailMessage(config, n)); err != nil {
		return fmt.Errorf("send the email: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("send the email: %w", err)
	}
	return client.Quit()
}

// dialSMTP connects to the mail server with the configured security.
func (s *Service) dialSMTP(ctx context.Context, config ChannelConfig) (*smtp.Client, error) {
	address := net.JoinHostPort(config.SMTPHost, strconv.Itoa(config.SMTPPort))
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if config.SMTPSecurity == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: config.SMTPHost, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("reach the mail server: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, config.SMTPHost)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("talk to the mail server: %w", err)
	}
	if config.SMTPSecurity == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: config.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("start TLS with the mail server: %w", err)
		}
	}
	return client, nil
}

func emailMessage(config ChannelConfig, n Notification) []byte {
	var message strings.Builder
	message.WriteString("From: " + config.From + "\r\n")
	message.WriteString("To: " + strings.Join(config.To, ", ") + "\r\n")
	message.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", "[Sentinel] "+n.title()) + "\r\n")
	message.WriteString("Date: " + n.At.Format(time.RFC1123Z) + "\r\n")
	message.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	body := fmt.Sprintf("%s\r\n\r\nMachine: %s\r\nRule: %s\r\nSeverity: %s\r\nTime: %s\r\n",
		n.Message, n.Machine, n.Rule, n.Severity, n.At.UTC().Format(time.RFC3339))
	message.WriteString(strings.ReplaceAll(body, "\r\n.", "\r\n.."))
	return []byte(message.String())
}

// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Channel types.
const (
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"
	ChannelNtfy    = "ntfy"
	ChannelDiscord = "discord"
)

// ErrExists means a channel with that name already exists.
var ErrExists = errors.New("a channel with that name already exists")

// ChannelConfig holds each channel type's settings. They are stored
// encrypted (SEC-42) and never sent back to the browser whole: passwords,
// tokens and webhook URLs (which carry their own tokens) are write-only.
type ChannelConfig struct {
	// Email over SMTP. Security is "starttls" (the default), "tls" (port
	// 465) or "none", for a relay on the same network.
	SMTPHost     string   `json:"smtpHost,omitempty"`
	SMTPPort     int      `json:"smtpPort,omitempty"`
	SMTPUsername string   `json:"smtpUsername,omitempty"`
	SMTPPassword string   `json:"smtpPassword,omitempty"`
	SMTPSecurity string   `json:"smtpSecurity,omitempty"`
	From         string   `json:"from,omitempty"`
	To           []string `json:"to,omitempty"`
	// URL is where a webhook or Discord notification is posted.
	URL string `json:"url,omitempty"`
	// ntfy: the server (https://ntfy.sh by default), the topic and an
	// optional access token.
	Server string `json:"server,omitempty"`
	Topic  string `json:"topic,omitempty"`
	Token  string `json:"token,omitempty"`
}

// Channel is one way of sending notifications.
type Channel struct {
	ID      int64
	Type    string
	Name    string
	Enabled bool
	Config  ChannelConfig
	// LastSentAt is when a notification last went out, LastError why the
	// last one failed ("" if it didn't).
	LastSentAt *int64
	LastError  string
}

// Summary describes where the channel sends, without its secrets.
func (c Channel) Summary() string {
	switch c.Type {
	case ChannelEmail:
		return fmt.Sprintf("%s to %s", c.Config.SMTPHost, strings.Join(c.Config.To, ", "))
	case ChannelNtfy:
		return fmt.Sprintf("topic %s on %s", c.Config.Topic, hostOf(c.Config.Server))
	}
	return "posts to " + hostOf(c.Config.URL)
}

func hostOf(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return "?"
	}
	return parsed.Host
}

// validate checks a channel and fills in its defaults.
func (c *Channel) validate() error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 100 {
		return &InvalidError{"name", "give a name of up to 100 characters"}
	}
	config := &c.Config
	switch c.Type {
	case ChannelEmail:
		return config.validateEmail()
	case ChannelWebhook, ChannelDiscord:
		*config = ChannelConfig{URL: strings.TrimSpace(config.URL)}
		return validURL("url", config.URL)
	case ChannelNtfy:
		if config.Server == "" {
			config.Server = "https://ntfy.sh"
		}
		*config = ChannelConfig{Server: strings.TrimRight(strings.TrimSpace(config.Server), "/"), Topic: strings.TrimSpace(config.Topic), Token: config.Token}
		if config.Topic == "" || strings.ContainsAny(config.Topic, "/?# ") {
			return &InvalidError{"topic", "give a topic name without slashes or spaces"}
		}
		return validURL("server", config.Server)
	}
	return &InvalidError{"type", "choose email, webhook, ntfy or discord"}
}

func (c *ChannelConfig) validateEmail() error {
	if c.SMTPSecurity == "" {
		c.SMTPSecurity = "starttls"
	}
	if c.SMTPPort == 0 {
		c.SMTPPort = map[string]int{"starttls": 587, "tls": 465, "none": 25}[c.SMTPSecurity]
	}
	switch {
	case strings.TrimSpace(c.SMTPHost) == "" || strings.ContainsAny(c.SMTPHost, " /:"):
		return &InvalidError{"smtpHost", "give the mail server's host name"}
	case c.SMTPSecurity != "starttls" && c.SMTPSecurity != "tls" && c.SMTPSecurity != "none":
		return &InvalidError{"smtpSecurity", "choose starttls, tls or none"}
	case c.SMTPPort < 1 || c.SMTPPort > 65535:
		return &InvalidError{"smtpPort", "choose a port from 1 to 65535"}
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return &InvalidError{"from", "give the address notifications come from"}
	}
	if len(c.To) == 0 {
		return &InvalidError{"to", "give at least one address to send to"}
	}
	for _, to := range c.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return &InvalidError{"to", fmt.Sprintf("%q isn't an email address", to)}
		}
	}
	*c = ChannelConfig{SMTPHost: c.SMTPHost, SMTPPort: c.SMTPPort, SMTPUsername: c.SMTPUsername, SMTPPassword: c.SMTPPassword,
		SMTPSecurity: c.SMTPSecurity, From: c.From, To: c.To}
	return nil
}

func validURL(field, address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return &InvalidError{field, "give a full http or https address"}
	}
	return nil
}

// keepSecrets fills in secrets left empty in an edit with the ones stored,
// since the browser never sees them to send them back.
func (c *ChannelConfig) keepSecrets(stored ChannelConfig) {
	if c.SMTPPassword == "" {
		c.SMTPPassword = stored.SMTPPassword
	}
	if c.Token == "" {
		c.Token = stored.Token
	}
	if c.URL == "" {
		c.URL = stored.URL
	}
}

// associatedData binds a channel's sealed settings to its type, so they
// can't be moved to a channel of another type.
func associatedData(channelType string) []byte {
	return []byte("notification_channel:" + channelType)
}

// Channels lists every channel, by name.
func (s *Service) Channels(ctx context.Context) ([]Channel, error) {
	var channels []Channel
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		channels, err = s.readChannels(ctx, tx, `SELECT `+channelColumns+` FROM notification_channels ORDER BY name COLLATE NOCASE`)
		return err
	})
	return channels, err
}

const channelColumns = `id, type, name, enabled, config_encrypted, last_sent_at, last_error`

func (s *Service) readChannels(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Channel, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	channels := []Channel{}
	for rows.Next() {
		var c Channel
		var sealed []byte
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.Enabled, &sealed, &c.LastSentAt, &c.LastError); err != nil {
			return nil, fmt.Errorf("read a channel: %w", err)
		}
		plain, err := s.sealer.Decrypt(sealed, associatedData(c.Type))
		if err != nil {
			return nil, fmt.Errorf("decrypt channel %d's settings: %w", c.ID, err)
		}
		if err := json.Unmarshal(plain, &c.Config); err != nil {
			return nil, fmt.Errorf("read channel %d's settings: %w", c.ID, err)
		}
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// SaveChannel adds a channel (ID 0) or changes one, and audits it.
func (s *Service) SaveChannel(ctx context.Context, actor auth.Session, request auth.RequestInfo, channel Channel) (Channel, error) {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return Channel{}, err
	}
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		if channel.ID != 0 {
			stored, err := s.readChannels(ctx, tx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, channel.ID)
			if err != nil {
				return err
			}
			if len(stored) == 0 {
				return ErrNotFound
			}
			channel.Config.keepSecrets(stored[0].Config)
		}
		if err := channel.validate(); err != nil {
			return err
		}
		if err := s.writeChannel(ctx, tx, &channel); err != nil {
			return err
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "alert.channel.save", map[string]any{
			"channel": channel.ID, "name": channel.Name, "type": channel.Type, "enabled": channel.Enabled,
		}))
	})
	return channel, err
}

func (s *Service) writeChannel(ctx context.Context, tx *sql.Tx, channel *Channel) error {
	plain, err := json.Marshal(channel.Config)
	if err != nil {
		return fmt.Errorf("save the channel: %w", err)
	}
	sealed, err := s.sealer.Encrypt(plain, associatedData(channel.Type))
	if err != nil {
		return fmt.Errorf("encrypt the channel's settings: %w", err)
	}
	if channel.ID == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO notification_channels (type, name, config_encrypted, enabled, created_at)
			VALUES (?, ?, ?, ?, ?)`, channel.Type, channel.Name, sealed, channel.Enabled, s.clock.Now().UnixMilli())
		if err == nil {
			channel.ID, err = result.LastInsertId()
		}
		return channelWriteError(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_channels SET type = ?, name = ?, config_encrypted = ?, enabled = ? WHERE id = ?`,
		channel.Type, channel.Name, sealed, channel.Enabled, channel.ID)
	return channelWriteError(err)
}

func channelWriteError(err error) error {
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		return ErrExists
	}
	return fmt.Errorf("save the channel: %w", err)
}

// DeleteChannel removes a channel; rules that used it no longer do.
func (s *Service) DeleteChannel(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete the channel: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return ErrNotFound
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "alert.channel.delete", map[string]any{"channel": id}))
	})
}

// TestChannel sends a test notification through a channel and says
// whether it went.
func (s *Service) TestChannel(ctx context.Context, actor auth.Session, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return err
	}
	var channels []Channel
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		channels, err = s.readChannels(ctx, tx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return ErrNotFound
	}
	err = s.deliver(ctx, channels[0], Notification{
		Event: EventTest, Machine: "Sentinel", Severity: Warning, Message: "This is a test notification from Sentinel.",
		Rule: "Test", At: s.clock.Now(),
	})
	if err != nil {
		return &DeliveryError{Err: err}
	}
	return nil
}

// DeliveryError says a notification couldn't be sent: the channel's
// server refused it or couldn't be reached.
type DeliveryError struct {
	Err error
}

func (e *DeliveryError) Error() string { return e.Err.Error() }

func (e *DeliveryError) Unwrap() error { return e.Err }

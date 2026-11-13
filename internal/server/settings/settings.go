// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// The release channels (§9.10).
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// The settings rows.
const (
	keyRetention = "metrics.retention"
	keyChannel   = "updates.channel"
)

// Bounds on retention, in the units the page uses.
const (
	minRawHours, maxRawHours     = 1, 7 * 24
	minMinuteDays, maxMinuteDays = 1, 90
	minHourDays, maxHourDays     = 30, 5 * 365
)

// Retention is how long metrics history is kept, per resolution.
type Retention struct {
	RawHours   int `json:"rawHours"`
	MinuteDays int `json:"minuteDays"`
	HourDays   int `json:"hourDays"`
}

// Settings are the values admins can change.
type Settings struct {
	Retention     Retention `json:"retention"`
	UpdateChannel string    `json:"updateChannel"`
}

// InvalidError says which setting is wrong, and why.
type InvalidError struct {
	Field, Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// Retainer applies a retention (metrics.Service).
type Retainer interface {
	SetRetention(retention metrics.Retention)
}

// StepUp checks a recent second-factor check (auth.Service, SEC-07).
type StepUp interface {
	RequireRecentSecondFactor(session auth.Session) error
}

// Options configure a Service.
type Options struct {
	Database Database
	AuditLog *audit.Log
	Metrics  Retainer
	StepUp   StepUp
	// ServerVersion picks the default channel: beta for a pre-release
	// or development build, stable otherwise.
	ServerVersion string
}

// Service reads and changes the settings.
type Service struct {
	options Options
}

// New returns a Service.
func New(options Options) *Service {
	return &Service{options: options}
}

// defaults are the settings nobody has changed.
func (s *Service) defaults() Settings {
	channel := ChannelStable
	if version := s.options.ServerVersion; version == "" || strings.Contains(version, "-") || strings.HasPrefix(version, "dev") {
		channel = ChannelBeta
	}
	return Settings{Retention: Retention{RawHours: 24, MinuteDays: 14, HourDays: 365}, UpdateChannel: channel}
}

// Apply loads the stored settings and puts the retention in force. The
// server calls it once at start.
func (s *Service) Apply(ctx context.Context) error {
	current, err := s.Get(ctx)
	if err != nil {
		return err
	}
	s.options.Metrics.SetRetention(current.Retention.metrics())
	return nil
}

// Get returns the settings, with defaults for those never changed.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	current := s.defaults()
	err := s.options.Database.Read(ctx, func(tx *sql.Tx) error {
		if err := readSetting(ctx, tx, keyRetention, &current.Retention); err != nil {
			return err
		}
		return readSetting(ctx, tx, keyChannel, &current.UpdateChannel)
	})
	return current, err
}

// Channel is the release channel agent builds come from.
func (s *Service) Channel(ctx context.Context) string {
	current, err := s.Get(ctx)
	if err != nil {
		return s.defaults().UpdateChannel
	}
	return current.UpdateChannel
}

// Update checks and saves new settings, needing an admin who checked their
// second factor recently (SEC-07), and audits what changed.
func (s *Service) Update(ctx context.Context, actor auth.Session, next Settings, request auth.RequestInfo) (Settings, error) {
	if err := auth.Authorize(actor, auth.PermissionManageSettings); err != nil {
		return Settings{}, err
	}
	if err := next.validate(); err != nil {
		return Settings{}, err
	}
	if err := s.options.StepUp.RequireRecentSecondFactor(actor); err != nil {
		return Settings{}, err
	}
	err := s.options.Database.Write(ctx, func(tx *sql.Tx) error {
		if err := writeSetting(ctx, tx, keyRetention, next.Retention); err != nil {
			return err
		}
		if err := writeSetting(ctx, tx, keyChannel, next.UpdateChannel); err != nil {
			return err
		}
		userID := actor.UserID
		return s.options.AuditLog.RecordInTx(ctx, tx, audit.Entry{
			UserID: &userID, Username: actor.Username, Action: "settings.change", Outcome: audit.OutcomeSuccess, IP: request.IP,
			Details: map[string]any{"retention": next.Retention, "updateChannel": next.UpdateChannel},
		})
	})
	if err != nil {
		return Settings{}, err
	}
	s.options.Metrics.SetRetention(next.Retention.metrics())
	return next, nil
}

func (s Settings) validate() error {
	r := s.Retention
	switch {
	case r.RawHours < minRawHours || r.RawHours > maxRawHours:
		return &InvalidError{"rawHours", "keep raw samples from 1 hour to 7 days"}
	case r.MinuteDays < minMinuteDays || r.MinuteDays > maxMinuteDays:
		return &InvalidError{"minuteDays", "keep minute averages from 1 to 90 days"}
	case r.HourDays < minHourDays || r.HourDays > maxHourDays:
		return &InvalidError{"hourDays", "keep hourly averages from 30 days to 5 years"}
	case r.MinuteDays*24 < r.RawHours || r.HourDays < r.MinuteDays:
		return &InvalidError{"retention", "keep coarser history at least as long as finer"}
	case s.UpdateChannel != ChannelStable && s.UpdateChannel != ChannelBeta:
		return &InvalidError{"updateChannel", "choose stable or beta"}
	}
	return nil
}

// metrics is the retention as the metrics service takes it.
func (r Retention) metrics() metrics.Retention {
	return metrics.Retention{
		Raw: time.Duration(r.RawHours) * time.Hour, Minute: time.Duration(r.MinuteDays) * 24 * time.Hour,
		Hour: time.Duration(r.HourDays) * 24 * time.Hour,
	}
}

func readSetting(ctx context.Context, tx *sql.Tx, key string, into any) error {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read setting %s: %w", key, err)
	}
	if err := json.Unmarshal([]byte(value), into); err != nil {
		return fmt.Errorf("read setting %s: %w", key, err)
	}
	return nil
}

func writeSetting(ctx context.Context, tx *sql.Tx, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, key, string(encoded))
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}

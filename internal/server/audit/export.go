// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Format is an export file format.
type Format string

// The supported export formats.
const (
	FormatCSV  Format = "csv"
	FormatJSON Format = "json"
)

// csvHeader names the CSV columns. Times are written in UTC, RFC 3339.
func csvHeader() []string {
	return []string{
		"id", "time", "user_id", "username", "agent_id", "action", "details", "outcome", "ip",
		"previous_hash", "hash",
	}
}

// Export writes the whole log, oldest first, to w in the given format. It
// streams entries as it reads them, so a large log doesn't need to fit in
// memory.
func (l *Log) Export(ctx context.Context, w io.Writer, format Format) error {
	switch format {
	case FormatCSV:
		return l.exportCSV(ctx, w)
	case FormatJSON:
		return l.exportJSON(ctx, w)
	default:
		return fmt.Errorf("unknown export format %q", format)
	}
}

func (l *Log) exportCSV(ctx context.Context, w io.Writer) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(csvHeader()); err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}

	err := l.All(ctx, func(record Record) error {
		return writer.Write([]string{
			strconv.FormatInt(record.ID, 10),
			time.UnixMilli(record.Time).UTC().Format(time.RFC3339Nano),
			optionalID(record.UserID),
			spreadsheetSafe(record.Username),
			optionalID(record.AgentID),
			spreadsheetSafe(record.Action),
			spreadsheetSafe(string(record.Details)),
			string(record.Outcome),
			spreadsheetSafe(record.IP),
			record.PreviousHash,
			record.Hash,
		})
	})
	if err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}
	return nil
}

func (l *Log) exportJSON(ctx context.Context, w io.Writer) error {
	if _, err := io.WriteString(w, "["); err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}

	encoder := json.NewEncoder(w)
	separator := "\n"
	err := l.All(ctx, func(record Record) error {
		if _, err := io.WriteString(w, separator); err != nil {
			return err
		}
		separator = ",\n"
		return encoder.Encode(record)
	})
	if err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}

	if _, err := io.WriteString(w, "]\n"); err != nil {
		return fmt.Errorf("export audit log: %w", err)
	}
	return nil
}

func optionalID(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// spreadsheetSafe stops a value from being run as a formula when the CSV is
// opened in a spreadsheet. Values like a username of "=HYPERLINK(...)" are
// otherwise executed by Excel and similar programs ("CSV injection"); a
// leading apostrophe makes them plain text.
// https://owasp.org/www-community/attacks/CSV_Injection
func spreadsheetSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

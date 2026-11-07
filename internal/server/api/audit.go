// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// AuditEntry is one audit log row (SEC-40).
type AuditEntry struct {
	ID       int64  `json:"id"`
	Time     int64  `json:"time"`
	UserID   *int64 `json:"userId"`
	Username string `json:"username"`
	AgentID  *int64 `json:"agentId"`
	Action   string `json:"action"`
	// Details holds action-specific facts as a JSON object.
	Details      json.RawMessage `json:"details" tstype:"Record<string, unknown>"`
	Outcome      string          `json:"outcome"`
	IP           string          `json:"ip"`
	PreviousHash string          `json:"previousHash"`
	Hash         string          `json:"hash"`
}

// AuditEntriesResponse is one page of the audit log, newest first.
type AuditEntriesResponse struct {
	Entries []AuditEntry `json:"entries"`
	// NextBefore is the "before" value for the next page, or null at the
	// oldest entry.
	NextBefore *int64 `json:"nextBefore"`
}

// AuditVerifyResponse is the result of checking the hash chain (SEC-41).
type AuditVerifyResponse struct {
	Checked    int64  `json:"checked"`
	Valid      bool   `json:"valid"`
	BrokenAtID int64  `json:"brokenAtId"`
	Problem    string `json:"problem"`
}

// defaultAuditPageSize is how many entries a page has unless asked otherwise.
const defaultAuditPageSize = 100

func (h *handlers) handleListAudit(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	options, ok := auditListOptions(w, r)
	if !ok {
		return
	}
	records, err := h.auditLog.List(r.Context(), options)
	if err != nil {
		h.writeError(w, err)
		return
	}

	response := AuditEntriesResponse{Entries: make([]AuditEntry, 0, len(records))}
	for _, record := range records {
		response.Entries = append(response.Entries, auditEntry(record))
	}
	if len(records) == options.Limit && records[len(records)-1].ID > 1 {
		next := records[len(records)-1].ID
		response.NextBefore = &next
	}
	writeJSON(w, http.StatusOK, response)
}

// auditListOptions reads ?before= and ?limit=, writing a 400 when either is
// out of range.
func auditListOptions(w http.ResponseWriter, r *http.Request) (audit.ListOptions, bool) {
	options := audit.ListOptions{Limit: defaultAuditPageSize}
	query := r.URL.Query()

	if value := query.Get("before"); value != "" {
		before, err := strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			writeInvalidInput(w, "before", "must be a positive number")
			return audit.ListOptions{}, false
		}
		options.BeforeID = before
	}

	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 || limit > audit.MaxPageSize {
			writeInvalidInput(w, "limit", "must be between 1 and 500")
			return audit.ListOptions{}, false
		}
		options.Limit = limit
	}
	return options, true
}

func (h *handlers) handleVerifyAudit(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	result, err := h.auditLog.Verify(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AuditVerifyResponse(result))
}

// handleExportAudit downloads the whole log as CSV or JSON (SEC-41). The
// export itself is audited, since it copies the log out of the server.
func (h *handlers) handleExportAudit(w http.ResponseWriter, r *http.Request, session auth.Session) {
	format := audit.Format(r.URL.Query().Get("format"))
	var contentType string
	switch format {
	case audit.FormatCSV:
		contentType = "text/csv; charset=utf-8"
	case audit.FormatJSON:
		contentType = "application/json"
	default:
		writeInvalidInput(w, "format", "choose csv or json")
		return
	}

	entry := audit.Entry{UserID: &session.UserID, Username: session.Username, Action: "audit.export",
		Outcome: audit.OutcomeSuccess, IP: requestInfo(r).IP, Details: map[string]any{"format": format}}
	if err := h.auditLog.Record(r.Context(), entry); err != nil {
		h.writeError(w, err)
		return
	}

	fileName := "sentinel-audit-" + h.clock.Now().UTC().Format("2006-01-02") + "." + string(format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := h.auditLog.Export(r.Context(), w, format); err != nil {
		// Headers and part of the file may already be sent; all that is left
		// is to tell the operator.
		h.logger.Error("audit export failed", "error", err)
	}
}

func auditEntry(record audit.Record) AuditEntry {
	return AuditEntry{
		ID: record.ID, Time: record.Time, UserID: record.UserID, Username: record.Username, AgentID: record.AgentID,
		Action: record.Action, Details: record.Details, Outcome: string(record.Outcome), IP: record.IP,
		PreviousHash: record.PreviousHash, Hash: record.Hash,
	}
}

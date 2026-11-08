// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// MetricSeries is one metric of one machine over a time range, for a chart.
type MetricSeries struct {
	Metric string `json:"metric"`
	// Resolution is "raw" (10-second samples), "1m" or "1h".
	Resolution string        `json:"resolution"`
	Points     []MetricPoint `json:"points"`
}

// MetricPoint is one point of a series. For raw samples Min and Max equal
// Value; for rollups they are the bucket's extremes.
type MetricPoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

// maxMetricRange is the longest range a chart can ask for: the hourly
// table's retention plus a day of slack.
const maxMetricRange = metrics.HourRetention + 24*time.Hour

// handleAgentMetrics answers GET /api/agents/{id}/metrics?metric=&from=&to=
// with from and to in Unix milliseconds.
func (h *handlers) handleAgentMetrics(w http.ResponseWriter, r *http.Request, session auth.Session) {
	query := r.URL.Query()
	from, ok := queryMilliseconds(w, query.Get("from"), "from")
	if !ok {
		return
	}
	to, ok := queryMilliseconds(w, query.Get("to"), "to")
	if !ok {
		return
	}
	if to-from > maxMetricRange.Milliseconds() {
		writeInvalidInput(w, "from", "must be at most a year before to")
		return
	}
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	metric := query.Get("metric")
	series, err := h.metrics.Query(r.Context(), agent.ID, metric, from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := MetricSeries{Metric: metric, Resolution: string(series.Resolution), Points: make([]MetricPoint, 0, len(series.Points))}
	for _, point := range series.Points {
		response.Points = append(response.Points, MetricPoint(point))
	}
	writeJSON(w, http.StatusOK, response)
}

func queryMilliseconds(w http.ResponseWriter, value, field string) (int64, bool) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds < 0 {
		writeInvalidInput(w, field, "must be a time in Unix milliseconds")
		return 0, false
	}
	return milliseconds, true
}

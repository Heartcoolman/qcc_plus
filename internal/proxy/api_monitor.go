package proxy

import (
	"context"
	"net/http"
	"sort"
	"time"

	"qcc_plus/internal/store"
)

type MonitorDashboardResponse struct {
	AccountID   string        `json:"account_id"`
	AccountName string        `json:"account_name"`
	Nodes       []MonitorNode `json:"nodes"`
	UpdatedAt   string        `json:"updated_at"`
}

type MonitorNode struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	URL             string       `json:"url"`
	Status          string       `json:"status"`
	Weight          int          `json:"weight"`
	IsActive        bool         `json:"is_active"`
	Disabled        bool         `json:"disabled"`
	SuccessRate     float64      `json:"success_rate"`
	AvgResponseTime int64        `json:"avg_response_time"`
	LastCheckAt     *string      `json:"last_check_at"`
	LastError       string       `json:"last_error"`
	LastPingMS      int64        `json:"last_ping_ms"`
	Trend24h        []TrendPoint `json:"trend_24h"`
	TotalRequests   int64        `json:"total_requests"`
	FailedRequests  int64        `json:"failed_requests"`
}

type TrendPoint struct {
	Timestamp   string  `json:"timestamp"`
	SuccessRate float64 `json:"success_rate"`
	AvgTime     int64   `json:"avg_time"`
}

type nodeSnapshot struct {
	ID        string
	Name      string
	URL       string
	Weight    int
	Failed    bool
	Disabled  bool
	LastError string
	Metrics   metrics
	CreatedAt time.Time
}

func (p *Server) handleMonitorDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	caller := accountFromCtx(r)
	if caller == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	target := caller
	if aid := r.URL.Query().Get("account_id"); aid != "" {
		if !isAdmin(r.Context()) && aid != caller.ID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		acc := p.getAccountByID(aid)
		if acc == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
			return
		}
		target = acc
	}

	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	resp := p.buildMonitorDashboardResponse(r.Context(), target)
	if resp == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "build dashboard failed"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (p *Server) buildMonitorDashboardResponse(ctx context.Context, target *Account) *MonitorDashboardResponse {
	if target == nil {
		return nil
	}

	var (
		snapshots   []nodeSnapshot
		activeID    string
		accountID   string
		accountName string
		nodeIDs     []string
	)

	p.mu.RLock()
	activeID = target.ActiveID
	accountID = target.ID
	accountName = target.Name
	for _, n := range target.Nodes {
		urlStr := ""
		if n.URL != nil {
			urlStr = n.URL.String()
		}
		nodeIDs = append(nodeIDs, n.ID)
		snapshots = append(snapshots, nodeSnapshot{
			ID:        n.ID,
			Name:      n.Name,
			URL:       urlStr,
			Weight:    n.Weight,
			Failed:    n.Failed,
			Disabled:  n.Disabled,
			LastError: n.LastError,
			Metrics:   n.Metrics,
			CreatedAt: n.CreatedAt,
		})
	}
	p.mu.RUnlock()

	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Weight != snapshots[j].Weight {
			return snapshots[i].Weight < snapshots[j].Weight
		}
		ti := snapshots[i].CreatedAt
		tj := snapshots[j].CreatedAt
		if ti.IsZero() || tj.IsZero() {
			return ti.IsZero() && !tj.IsZero()
		}
		return ti.Before(tj)
	})

	trendRecords := make(map[string][]store.MetricsRecord, len(snapshots))
	if p.store != nil && len(nodeIDs) > 0 {
		recs, err := p.store.GetNodes24hTrend(ctx, accountID, nodeIDs)
		if err != nil {
			if p.logger != nil {
				p.logger.Printf("get trend failed account=%s: %v", accountID, err)
			}
		} else {
			trendRecords = recs
		}
	}

	nodes := make([]MonitorNode, 0, len(snapshots))
	for _, snap := range snapshots {
		successCount := snap.Metrics.Requests - snap.Metrics.FailCount
		if successCount < 0 {
			successCount = 0
		}
		status := "offline"
		if !snap.Failed && !snap.Disabled {
			status = "online"
		}
		lastError := snap.LastError
		if lastError == "" {
			lastError = snap.Metrics.LastPingErr
		}
		var lastCheck *string
		if !snap.Metrics.LastHealthCheckAt.IsZero() {
			ts := snap.Metrics.LastHealthCheckAt.UTC().Format(time.RFC3339)
			lastCheck = &ts
		}
		totalDuration := snap.Metrics.FirstByteDur + snap.Metrics.StreamDur
		nodes = append(nodes, MonitorNode{
			ID:              snap.ID,
			Name:            snap.Name,
			URL:             snap.URL,
			Status:          status,
			Weight:          snap.Weight,
			IsActive:        snap.ID == activeID,
			Disabled:        snap.Disabled,
			SuccessRate:     calculateSuccessRate(successCount, snap.Metrics.FailCount),
			AvgResponseTime: calculateAvgResponseTime(totalDuration.Milliseconds(), snap.Metrics.Requests),
			LastCheckAt:     lastCheck,
			LastError:       lastError,
			LastPingMS:      snap.Metrics.LastPingMS,
			Trend24h:        buildTrendPoints(trendRecords[snap.ID]),
			TotalRequests:   snap.Metrics.Requests,
			FailedRequests:  snap.Metrics.FailCount,
		})
	}

	name := accountName
	if name == "" {
		name = accountID
	}

	resp := MonitorDashboardResponse{
		AccountID:   accountID,
		AccountName: name,
		Nodes:       nodes,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	return &resp
}

func calculateSuccessRate(successCount, failCount int64) float64 {
	total := successCount + failCount
	if total == 0 {
		return 100.0
	}
	return float64(successCount) / float64(total) * 100.0
}

func calculateAvgResponseTime(sumMS, count int64) int64 {
	if count == 0 {
		return 0
	}
	return sumMS / count
}

func buildTrendPoints(records []store.MetricsRecord) []TrendPoint {
	points := make([]TrendPoint, 0, len(records))
	for _, rec := range records {
		points = append(points, TrendPoint{
			Timestamp:   rec.Timestamp.UTC().Format(time.RFC3339),
			SuccessRate: calculateSuccessRate(rec.RequestsSuccess, rec.RequestsFailed),
			AvgTime:     calculateAvgResponseTime(rec.ResponseTimeSumMs, rec.ResponseTimeCount),
		})
	}
	return points
}

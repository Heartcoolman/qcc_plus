package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"qcc_plus/internal/store"
)

// respondJSON 保持与其他 API 一致的 JSON 响应格式。
func respondJSON(w http.ResponseWriter, status int, v interface{}) {
	writeJSON(w, status, v)
}

// handleGetNodeMetrics 处理 GET /api/nodes/:id/metrics
func (p *Server) handleGetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if p.store == nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "metrics store not enabled"})
		return
	}

	nodeID, ok := extractNodeIDFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	acc := accountFromCtx(r)
	if acc == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	node := p.getNode(nodeID)
	if node == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	// 非管理员只能查询自己账号的节点
	if !isAdmin(r.Context()) && node.AccountID != acc.ID {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	gran, from, to, limit, offset, err := parseMetricsQueryParams(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	q := store.MetricsQuery{
		AccountID:   node.AccountID,
		NodeID:      nodeID,
		From:        from,
		To:          to,
		Granularity: gran,
		Limit:       limit,
		Offset:      offset,
	}
	records, err := p.store.QueryMetrics(r.Context(), q)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	data := make([]map[string]interface{}, 0, len(records))
	for _, rec := range records {
		avgResp := safeDiv(rec.ResponseTimeSumMs, rec.ResponseTimeCount)
		avgFirst := safeDiv(rec.FirstByteTimeSumMs, rec.ResponseTimeCount)
		avgStream := safeDiv(rec.StreamDurationSumMs, rec.ResponseTimeCount)
		data = append(data, map[string]interface{}{
			"timestamp":              rec.Timestamp.UTC().Format(time.RFC3339),
			"requests_total":         rec.RequestsTotal,
			"requests_success":       rec.RequestsSuccess,
			"requests_failed":        rec.RequestsFailed,
			"avg_response_time_ms":   avgResp,
			"bytes_total":            rec.BytesTotal,
			"input_tokens":           rec.InputTokensTotal,
			"output_tokens":          rec.OutputTokensTotal,
			"avg_first_byte_ms":      avgFirst,
			"avg_stream_duration_ms": avgStream,
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data":        data,
		"granularity": string(gran),
		"from":        from.UTC().Format(time.RFC3339),
		"to":          to.UTC().Format(time.RFC3339),
	})
}

// handleGetAccountMetrics 处理 GET /api/accounts/:id/metrics
func (p *Server) handleGetAccountMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if p.store == nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "metrics store not enabled"})
		return
	}

	accountID, ok := extractAccountIDFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	caller := accountFromCtx(r)
	if caller == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !isAdmin(r.Context()) && caller.ID != accountID {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	gran, from, to, limit, offset, err := parseMetricsQueryParams(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	q := store.MetricsQuery{
		AccountID:   accountID,
		Granularity: gran,
		From:        from,
		To:          to,
		Limit:       0, // 聚合账号需先取全量再分页
		Offset:      0,
	}
	records, err := p.store.QueryMetrics(r.Context(), q)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	agg := make(map[time.Time]store.MetricsRecord)
	for _, rec := range records {
		ts := rec.Timestamp.UTC()
		cur := agg[ts]
		cur.Timestamp = ts
		cur.RequestsTotal += rec.RequestsTotal
		cur.RequestsSuccess += rec.RequestsSuccess
		cur.RequestsFailed += rec.RequestsFailed
		cur.ResponseTimeSumMs += rec.ResponseTimeSumMs
		cur.ResponseTimeCount += rec.ResponseTimeCount
		cur.BytesTotal += rec.BytesTotal
		cur.InputTokensTotal += rec.InputTokensTotal
		cur.OutputTokensTotal += rec.OutputTokensTotal
		cur.FirstByteTimeSumMs += rec.FirstByteTimeSumMs
		cur.StreamDurationSumMs += rec.StreamDurationSumMs
		agg[ts] = cur
	}

	keys := make([]time.Time, 0, len(agg))
	for ts := range agg {
		keys = append(keys, ts)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	start := offset
	if start > len(keys) {
		start = len(keys)
	}
	end := len(keys)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	data := make([]map[string]interface{}, 0, end-start)
	for _, ts := range keys[start:end] {
		rec := agg[ts]
		data = append(data, map[string]interface{}{
			"timestamp":              ts.Format(time.RFC3339),
			"requests_total":         rec.RequestsTotal,
			"requests_success":       rec.RequestsSuccess,
			"requests_failed":        rec.RequestsFailed,
			"avg_response_time_ms":   safeDiv(rec.ResponseTimeSumMs, rec.ResponseTimeCount),
			"bytes_total":            rec.BytesTotal,
			"input_tokens":           rec.InputTokensTotal,
			"output_tokens":          rec.OutputTokensTotal,
			"avg_first_byte_ms":      safeDiv(rec.FirstByteTimeSumMs, rec.ResponseTimeCount),
			"avg_stream_duration_ms": safeDiv(rec.StreamDurationSumMs, rec.ResponseTimeCount),
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data":        data,
		"granularity": string(gran),
		"from":        from.UTC().Format(time.RFC3339),
		"to":          to.UTC().Format(time.RFC3339),
	})
}

// handleAggregateMetrics 处理 POST /api/metrics/aggregate
func (p *Server) handleAggregateMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isAdmin(r.Context()) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if p.store == nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "metrics store not enabled"})
		return
	}

	var req struct {
		Target    string `json:"target"`
		AccountID string `json:"account_id"`
		From      string `json:"from"`
		To        string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	target, err := parseAggregateTarget(req.Target)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	from, err := parseTime(req.From)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from"})
		return
	}
	to, err := parseTime(req.To)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to"})
		return
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "from must be before to"})
		return
	}

	if err := p.store.AggregateMetrics(r.Context(), req.AccountID, target, from, to); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCleanupMetrics 处理 POST /api/metrics/cleanup
func (p *Server) handleCleanupMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isAdmin(r.Context()) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if p.store == nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "metrics store not enabled"})
		return
	}

	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	if err := p.store.CleanupMetrics(r.Context(), req.AccountID, time.Now().UTC()); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseMetricsQueryParams 提取并校验查询参数，返回有效值与默认时间窗口。
func parseMetricsQueryParams(r *http.Request) (store.MetricsGranularity, time.Time, time.Time, int, int, error) {
	gran, err := parseGranularity(r.URL.Query().Get("granularity"))
	if err != nil {
		return "", time.Time{}, time.Time{}, 0, 0, err
	}

	from, err := parseTime(r.URL.Query().Get("from"))
	if err != nil {
		return "", time.Time{}, time.Time{}, 0, 0, err
	}
	to, err := parseTime(r.URL.Query().Get("to"))
	if err != nil {
		return "", time.Time{}, time.Time{}, 0, 0, err
	}

	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = defaultFrom(gran, to)
	}
	if !from.Before(to) {
		return "", time.Time{}, time.Time{}, 0, 0, fmt.Errorf("from must be before to")
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", time.Time{}, time.Time{}, 0, 0, fmt.Errorf("invalid limit")
		}
		if n > 0 {
			limit = n
		}
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", time.Time{}, time.Time{}, 0, 0, fmt.Errorf("invalid offset")
		}
		offset = n
	}

	return gran, from, to, limit, offset, nil
}

func parseGranularity(val string) (store.MetricsGranularity, error) {
	switch strings.ToLower(val) {
	case "", string(store.MetricsGranularityRaw):
		return store.MetricsGranularityRaw, nil
	case string(store.MetricsGranularityHourly):
		return store.MetricsGranularityHourly, nil
	case string(store.MetricsGranularityDaily):
		return store.MetricsGranularityDaily, nil
	case string(store.MetricsGranularityMonthly):
		return store.MetricsGranularityMonthly, nil
	default:
		return "", fmt.Errorf("unsupported granularity")
	}
}

func defaultFrom(gr store.MetricsGranularity, to time.Time) time.Time {
	switch gr {
	case store.MetricsGranularityHourly:
		return to.Add(-7 * 24 * time.Hour)
	case store.MetricsGranularityDaily:
		return to.AddDate(0, 0, -30)
	case store.MetricsGranularityMonthly:
		return to.AddDate(-1, 0, 0)
	default:
		return to.Add(-24 * time.Hour)
	}
}

func parseTime(val string) (time.Time, error) {
	if val == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func parseAggregateTarget(val string) (store.MetricsGranularity, error) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case string(store.MetricsGranularityHourly):
		return store.MetricsGranularityHourly, nil
	case string(store.MetricsGranularityDaily):
		return store.MetricsGranularityDaily, nil
	case string(store.MetricsGranularityMonthly):
		return store.MetricsGranularityMonthly, nil
	default:
		return "", fmt.Errorf("target must be hour|day|month")
	}
}

func extractNodeIDFromPath(path string) (string, bool) {
	// 期望格式：/api/nodes/{id}/metrics
	if !strings.HasPrefix(path, "/api/nodes/") || !strings.HasSuffix(path, "/metrics") {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, "/api/nodes/")
	trimmed = strings.TrimSuffix(trimmed, "/metrics")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func extractAccountIDFromPath(path string) (string, bool) {
	// 期望格式：/api/accounts/{id}/metrics
	if !strings.HasPrefix(path, "/api/accounts/") || !strings.HasSuffix(path, "/metrics") {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, "/api/accounts/")
	trimmed = strings.TrimSuffix(trimmed, "/metrics")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func safeDiv(sum int64, count int64) float64 {
	if count <= 0 {
		return 0
	}
	return float64(sum) / float64(count)
}

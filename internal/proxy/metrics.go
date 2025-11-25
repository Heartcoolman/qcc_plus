package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"qcc_plus/internal/store"
)

type metricsWriter struct {
	http.ResponseWriter
	firstWrite bool
	firstAt    time.Time
	lastAt     time.Time
	bytes      int64
	status     int
}

func (mw *metricsWriter) Header() http.Header { return mw.ResponseWriter.Header() }

func (mw *metricsWriter) WriteHeader(code int) {
	mw.status = code
	mw.ResponseWriter.WriteHeader(code)
}

func (mw *metricsWriter) Write(b []byte) (int, error) {
	if !mw.firstWrite {
		mw.firstWrite = true
		mw.firstAt = time.Now()
		if mw.status == 0 {
			mw.status = http.StatusOK
		}
	}
	mw.lastAt = time.Now()
	mw.bytes += int64(len(b))
	return mw.ResponseWriter.Write(b)
}

func (p *Server) recordMetrics(nodeID string, start time.Time, mw *metricsWriter, u *usage) {
	end := time.Now()
	var (
		nodeRec    store.NodeRecord
		metricsRec *store.MetricsRecord
	)

	p.mu.Lock()
	node, ok := p.nodeIndex[nodeID]
	if !ok {
		p.mu.Unlock()
		return
	}
	acc := p.nodeAccount[nodeID]
	accountID := node.AccountID
	if acc != nil && acc.ID != "" {
		accountID = acc.ID
	}
	if accountID == "" {
		accountID = store.DefaultAccountID
	}
	node.Metrics.Requests++
	if mw != nil && mw.firstWrite {
		node.Metrics.FirstByteDur += mw.firstAt.Sub(start)
		node.Metrics.StreamDur += mw.lastAt.Sub(mw.firstAt)
		node.Metrics.TotalBytes += mw.bytes
	}
	if u != nil {
		node.Metrics.TotalInputTokens += u.input
		node.Metrics.TotalOutputTokens += u.output
	}
	if mw != nil && mw.status != http.StatusOK {
		node.Metrics.FailCount++
		node.Metrics.FailStreak++
	}
	if mw != nil && mw.status == http.StatusOK {
		node.Metrics.FailStreak = 0
		node.LastError = ""
		node.Failed = false
		if acc != nil {
			delete(acc.FailedSet, nodeID)
		}
	}
	if p.store != nil {
		nodeRec = toRecord(node)
		metricsRec = buildMetricsRecord(accountID, nodeID, start, end, mw, u)
	}
	p.mu.Unlock()

	if p.store != nil {
		_ = p.store.UpsertNode(context.Background(), nodeRec)
		if metricsRec != nil {
			_ = p.store.InsertMetrics(context.Background(), *metricsRec)
		}
	}
}

func buildMetricsRecord(accountID, nodeID string, start, end time.Time, mw *metricsWriter, u *usage) *store.MetricsRecord {
	rec := &store.MetricsRecord{
		AccountID:         accountID,
		NodeID:            nodeID,
		Timestamp:         end.UTC(),
		RequestsTotal:     1,
		RequestsSuccess:   1,
		RequestsFailed:    0,
		ResponseTimeSumMs: end.Sub(start).Milliseconds(),
		ResponseTimeCount: 1,
	}
	if mw != nil {
		rec.BytesTotal = mw.bytes
		if mw.status != http.StatusOK {
			rec.RequestsFailed = 1
			rec.RequestsSuccess = 0
		}
		if mw.firstWrite {
			rec.FirstByteTimeSumMs = mw.firstAt.Sub(start).Milliseconds()
			rec.StreamDurationSumMs = mw.lastAt.Sub(mw.firstAt).Milliseconds()
			rec.ResponseTimeSumMs = mw.lastAt.Sub(start).Milliseconds()
		}
	}
	if u != nil {
		rec.InputTokensTotal = u.input
		rec.OutputTokensTotal = u.output
	}
	return rec
}

// 从响应体或 SSE 数据中粗略提取 usage 字段（JSON 格式）。
func parseUsage(b []byte) (int64, int64) {
	key := []byte("\"usage\"")
	idx := bytes.LastIndex(b, key)
	if idx < 0 {
		return 0, 0
	}
	braceStart := bytes.IndexByte(b[idx:], '{')
	if braceStart < 0 {
		return 0, 0
	}
	braceStart += idx
	depth := 0
	for i := braceStart; i < len(b); i++ {
		switch b[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				usageObj := b[braceStart : i+1]
				var tmp struct {
					InputTokens  int64 `json:"input_tokens"`
					OutputTokens int64 `json:"output_tokens"`
				}
				if err := json.Unmarshal(usageObj, &tmp); err == nil {
					return tmp.InputTokens, tmp.OutputTokens
				}
				break
			}
		}
	}
	return 0, 0
}

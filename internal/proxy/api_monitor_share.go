package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qcc_plus/internal/store"
)

type CreateMonitorShareRequest struct {
	AccountID string `json:"account_id"` // 可选，管理员可指定，普通用户只能创建自己的
	ExpireIn  string `json:"expire_in"`  // "1h", "24h", "168h"(7天), "permanent"
}

type CreateMonitorShareResponse struct {
	ID        string  `json:"id"`
	Token     string  `json:"token"`
	ShareURL  string  `json:"share_url"`
	ExpireAt  *string `json:"expire_at"` // RFC3339，永久时为 null
	CreatedAt string  `json:"created_at"`
}

func (p *Server) handleMonitorShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p.handleCreateMonitorShare(w, r)
	case http.MethodGet:
		p.handleListMonitorShares(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// POST /api/monitor/shares
func (p *Server) handleCreateMonitorShare(w http.ResponseWriter, r *http.Request) {
	if p.store == nil {
		shareError(w, http.StatusInternalServerError, "STORE_DISABLED", "store not enabled")
		return
	}
	caller := accountFromCtx(r)
	if caller == nil {
		shareError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}

	var req CreateMonitorShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shareError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid json")
		return
	}

	target := caller
	if req.AccountID != "" && req.AccountID != caller.ID {
		if !isAdmin(r.Context()) {
			shareError(w, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		if acc := p.getAccountByID(req.AccountID); acc != nil {
			target = acc
		} else {
			shareError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
			return
		}
	}

	expireAt, err := parseShareExpire(req.ExpireIn)
	if err != nil {
		shareError(w, http.StatusBadRequest, "INVALID_EXPIRE", err.Error())
		return
	}

	token, err := generateShareToken()
	if err != nil {
		shareError(w, http.StatusInternalServerError, "TOKEN_GEN_FAILED", "token generation failed")
		return
	}

	now := time.Now().UTC()
	rec := store.MonitorShareRecord{
		ID:        fmt.Sprintf("share-%d", now.UnixNano()),
		AccountID: target.ID,
		Token:     token,
		CreatedBy: caller.Name,
		CreatedAt: now,
	}
	if !expireAt.IsZero() {
		rec.ExpireAt = expireAt
	}

	if err := p.store.CreateMonitorShare(r.Context(), rec); err != nil {
		shareError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	expireStr := (*string)(nil)
	if !expireAt.IsZero() {
		s := expireAt.UTC().Format(time.RFC3339)
		expireStr = &s
	}
	shareURL := buildShareURL(r, token)
	resp := CreateMonitorShareResponse{
		ID:        rec.ID,
		Token:     token,
		ShareURL:  shareURL,
		ExpireAt:  expireStr,
		CreatedAt: rec.CreatedAt.Format(time.RFC3339),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// GET /api/monitor/shares
func (p *Server) handleListMonitorShares(w http.ResponseWriter, r *http.Request) {
	if p.store == nil {
		shareError(w, http.StatusInternalServerError, "STORE_DISABLED", "store not enabled")
		return
	}
	caller := accountFromCtx(r)
	if caller == nil {
		shareError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}

	targetID := caller.ID
	if aid := r.URL.Query().Get("account_id"); aid != "" {
		if !isAdmin(r.Context()) && aid != caller.ID {
			shareError(w, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		targetID = aid
	} else if isAdmin(r.Context()) {
		// 管理员未指定则查看全部
		targetID = ""
	}

	includeRevoked := false
	if v := r.URL.Query().Get("include_revoked"); v != "" {
		includeRevoked = strings.EqualFold(v, "true") || v == "1"
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	params := store.QueryMonitorSharesParams{
		AccountID:      targetID,
		IncludeRevoked: includeRevoked,
		Limit:          limit,
		Offset:         offset,
	}
	if isAdmin(r.Context()) && targetID == "" {
		params.AccountID = ""
	}

	records, err := p.store.ListMonitorShares(r.Context(), params)
	if err != nil {
		shareError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	resp := make([]map[string]interface{}, 0, len(records))
	for _, rec := range records {
		expireStr := (*string)(nil)
		if !rec.ExpireAt.IsZero() {
			s := rec.ExpireAt.UTC().Format(time.RFC3339)
			expireStr = &s
		}
		var revokedAt *string
		if rec.RevokedAt != nil {
			s := rec.RevokedAt.UTC().Format(time.RFC3339)
			revokedAt = &s
		}
		resp = append(resp, map[string]interface{}{
			"id":         rec.ID,
			"account_id": rec.AccountID,
			"token":      rec.Token,
			"share_url":  buildShareURL(r, rec.Token),
			"expire_at":  expireStr,
			"created_at": rec.CreatedAt.UTC().Format(time.RFC3339),
			"created_by": rec.CreatedBy,
			"revoked":    rec.Revoked,
			"revoked_at": revokedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"shares": resp})
}

// DELETE /api/monitor/shares/:id
func (p *Server) handleRevokeMonitorShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if p.store == nil {
		shareError(w, http.StatusInternalServerError, "STORE_DISABLED", "store not enabled")
		return
	}
	caller := accountFromCtx(r)
	if caller == nil {
		shareError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/monitor/shares/")
	id = strings.Trim(id, "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	rec, err := p.store.GetMonitorShareByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			shareError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "share not found")
			return
		}
		shareError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}
	if !isAdmin(r.Context()) && rec.AccountID != caller.ID {
		shareError(w, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}

	if err := p.store.RevokeMonitorShare(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			shareError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "share not found")
			return
		}
		shareError(w, http.StatusInternalServerError, "REVOKE_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/monitor/share/:token
func (p *Server) handleAccessMonitorShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if p.store == nil {
		shareError(w, http.StatusInternalServerError, "STORE_DISABLED", "store not enabled")
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/api/monitor/share/")
	token = strings.Trim(token, "/")
	if token == "" {
		shareError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "share not found")
		return
	}

	rec, err := p.store.GetMonitorShareByToken(r.Context(), token)
	if err != nil {
		shareError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}
	if rec == nil {
		shareError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "share not found")
		return
	}

	acc := p.getAccountByID(rec.AccountID)
	if acc == nil {
		shareError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
		return
	}

	resp := p.buildMonitorDashboardResponse(r.Context(), acc)
	if resp == nil {
		shareError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "build dashboard failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseShareExpire(val string) (time.Time, error) {
	now := time.Now().UTC()
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1h":
		return now.Add(time.Hour), nil
	case "24h":
		return now.Add(24 * time.Hour), nil
	case "168h":
		return now.Add(168 * time.Hour), nil
	case "permanent":
		return time.Time{}, nil
	case "":
		return time.Time{}, errors.New("expire_in required")
	default:
		return time.Time{}, fmt.Errorf("unsupported expire_in: %s", val)
	}
}

func shareError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{
		"error": msg,
		"code":  code,
	})
}

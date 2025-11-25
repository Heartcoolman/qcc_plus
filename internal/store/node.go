package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) UpsertNode(ctx context.Context, r NodeRecord) error {
	r.AccountID = normalizeAccount(r.AccountID)
	if r.HealthCheckMethod == "" {
		r.HealthCheckMethod = "api"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	healthAt := sql.NullTime{}
	if !r.LastHealthCheckAt.IsZero() {
		healthAt.Valid = true
		healthAt.Time = r.LastHealthCheckAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO nodes (id,name,base_url,api_key,health_check_method,account_id,weight,sort_order,failed,disabled,last_error,created_at,requests,fail_count,fail_streak,total_bytes,total_input,total_output,stream_dur_ms,first_byte_ms,last_ping_ms,last_ping_err,last_health_check_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			base_url=VALUES(base_url),
			api_key=VALUES(api_key),
			health_check_method=VALUES(health_check_method),
			account_id=VALUES(account_id),
			weight=VALUES(weight),
			sort_order=VALUES(sort_order),
			failed=VALUES(failed),
			disabled=VALUES(disabled),
			last_error=VALUES(last_error),
			last_ping_ms=VALUES(last_ping_ms),
			last_ping_err=VALUES(last_ping_err),
			last_health_check_at=VALUES(last_health_check_at)`,
		r.ID, r.Name, r.BaseURL, r.APIKey, r.HealthCheckMethod, r.AccountID, r.Weight, r.SortOrder, r.Failed, r.Disabled, r.LastError, r.CreatedAt, r.Requests, r.FailCount, r.FailStreak, r.TotalBytes, r.TotalInput, r.TotalOutput, r.StreamDurMs, r.FirstByteMs, r.LastPingMs, r.LastPingErr, healthAt)
	return err
}

func (s *Store) GetNodesByAccount(ctx context.Context, accountID string) ([]NodeRecord, error) {
	accountID = normalizeAccount(accountID)
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,base_url,api_key,health_check_method,account_id,weight,sort_order,failed,disabled,last_error,created_at,requests,fail_count,fail_streak,total_bytes,total_input,total_output,stream_dur_ms,first_byte_ms,last_ping_ms,last_ping_err,last_health_check_at FROM nodes WHERE account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []NodeRecord
	for rows.Next() {
		var r NodeRecord
		var lastHealthAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.BaseURL, &r.APIKey, &r.HealthCheckMethod, &r.AccountID, &r.Weight, &r.SortOrder, &r.Failed, &r.Disabled, &r.LastError, &r.CreatedAt, &r.Requests, &r.FailCount, &r.FailStreak, &r.TotalBytes, &r.TotalInput, &r.TotalOutput, &r.StreamDurMs, &r.FirstByteMs, &r.LastPingMs, &r.LastPingErr, &lastHealthAt); err != nil {
			return nil, err
		}
		if r.HealthCheckMethod == "" {
			r.HealthCheckMethod = "api"
		}
		if lastHealthAt.Valid {
			r.LastHealthCheckAt = lastHealthAt.Time
		}
		records = append(records, r)
	}
	return records, nil
}

func (s *Store) DeleteNode(ctx context.Context, id string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id)
	return err
}

// MaxSortOrder 返回指定账号当前的最大排序值。
func (s *Store) MaxSortOrder(ctx context.Context, accountID string) (int, error) {
	accountID = normalizeAccount(accountID)
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM nodes WHERE account_id=?`, accountID).Scan(&max); err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

// UpdateNodeOrders 批量更新节点排序。
func (s *Store) UpdateNodeOrders(ctx context.Context, accountID string, orders map[string]int) error {
	if len(orders) == 0 {
		return nil
	}
	accountID = normalizeAccount(accountID)
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE nodes SET sort_order=? WHERE id=? AND account_id=?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for id, order := range orders {
		if order < 0 {
			order = 0
		}
		if _, err := stmt.ExecContext(ctx, order, id, accountID); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

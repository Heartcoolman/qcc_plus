package proxy

import (
	"context"
	"log"
	"sync"
	"time"

	"qcc_plus/internal/store"
)

const (
	defaultAggregateInterval = time.Hour
	defaultCleanupInterval   = 24 * time.Hour
	cleanupHour              = 2 // 02:00 UTC
)

// MetricsScheduler 负责周期性聚合与清理监控数据。
type MetricsScheduler struct {
	store  *store.Store
	logger *log.Logger
	stopCh chan struct{}
	wg     sync.WaitGroup

	aggregateInterval time.Duration
	cleanupInterval   time.Duration
	stopOnce          sync.Once
}

// NewMetricsScheduler 创建调度器，默认每小时聚合、每天清理一次。
func NewMetricsScheduler(s *store.Store, logger *log.Logger) *MetricsScheduler {
	if logger == nil {
		logger = log.Default()
	}
	return &MetricsScheduler{
		store:             s,
		logger:            logger,
		stopCh:            make(chan struct{}),
		aggregateInterval: defaultAggregateInterval,
		cleanupInterval:   defaultCleanupInterval,
	}
}

// Start 启动定时任务。
func (m *MetricsScheduler) Start() error {
	if m == nil || m.store == nil {
		return nil
	}
	if m.aggregateInterval <= 0 {
		m.aggregateInterval = defaultAggregateInterval
	}
	if m.cleanupInterval <= 0 {
		m.cleanupInterval = defaultCleanupInterval
	}

	m.wg.Add(2)
	go m.aggregateLoop()
	go m.cleanupLoop()
	return nil
}

// Stop 发送停止信号并等待任务退出，最多等待 30 秒。
func (m *MetricsScheduler) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		m.logger.Printf("[MetricsScheduler] stop timeout, exiting forcefully")
	}
}

func (m *MetricsScheduler) aggregateLoop() {
	defer m.wg.Done()
	defer m.recoverPanic("aggregation loop")

	initialDelay := m.nextAggregateDelay(time.Now().UTC())
	timer := time.NewTimer(initialDelay)
	select {
	case <-m.stopCh:
		timer.Stop()
		return
	case <-timer.C:
		m.runAggregation()
	}

	ticker := time.NewTicker(m.aggregateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.runAggregation()
		}
	}
}

func (m *MetricsScheduler) cleanupLoop() {
	defer m.wg.Done()
	defer m.recoverPanic("cleanup loop")

	initialDelay := m.nextCleanupDelay(time.Now().UTC())
	timer := time.NewTimer(initialDelay)
	select {
	case <-m.stopCh:
		timer.Stop()
		return
	case <-timer.C:
		m.runCleanup()
	}

	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.runCleanup()
		}
	}
}

func (m *MetricsScheduler) runAggregation() {
	start := time.Now()
	m.logger.Printf("[MetricsScheduler] Starting hourly aggregation...")

	ctx, cancel := m.taskContext(30 * time.Second)
	defer cancel()

	now := time.Now().UTC()

	// 原始 -> 小时，过去 2 小时的数据。
	if err := m.store.AggregateMetrics(ctx, "", store.MetricsGranularityHourly, now.Add(-2*time.Hour), now); err != nil {
		m.logger.Printf("[MetricsScheduler] Aggregation failed (raw->hour): %v", err)
	}

	// 小时 -> 天，昨天的数据。
	yesterdayStart := startOfDay(now).Add(-24 * time.Hour)
	todayStart := startOfDay(now)
	if err := m.store.AggregateMetrics(ctx, "", store.MetricsGranularityDaily, yesterdayStart, todayStart); err != nil {
		m.logger.Printf("[MetricsScheduler] Aggregation failed (hour->day): %v", err)
	}

	// 天 -> 月，上个月的数据。
	currentMonthStart := startOfMonth(now)
	lastMonthStart := currentMonthStart.AddDate(0, -1, 0)
	if err := m.store.AggregateMetrics(ctx, "", store.MetricsGranularityMonthly, lastMonthStart, currentMonthStart); err != nil {
		m.logger.Printf("[MetricsScheduler] Aggregation failed (day->month): %v", err)
	}

	m.logger.Printf("[MetricsScheduler] Aggregation completed in %v", time.Since(start))
}

func (m *MetricsScheduler) runCleanup() {
	start := time.Now()
	m.logger.Printf("[MetricsScheduler] Starting daily cleanup...")

	ctx, cancel := m.taskContext(30 * time.Second)
	defer cancel()

	if err := m.store.CleanupMetrics(ctx, "", time.Now().UTC()); err != nil {
		m.logger.Printf("[MetricsScheduler] Cleanup failed: %v", err)
	} else {
		m.logger.Printf("[MetricsScheduler] Cleanup completed in %v", time.Since(start))
	}

	if err := m.store.CleanupHealthChecks(ctx, time.Time{}); err != nil {
		m.logger.Printf("[MetricsScheduler] Health history cleanup failed: %v", err)
	}
}

func (m *MetricsScheduler) nextAggregateDelay(now time.Time) time.Duration {
	if m.aggregateInterval <= 0 {
		return defaultAggregateInterval
	}
	next := now.Truncate(m.aggregateInterval).Add(m.aggregateInterval)
	return next.Sub(now)
}

func (m *MetricsScheduler) nextCleanupDelay(now time.Time) time.Duration {
	if m.cleanupInterval >= 20*time.Hour {
		next := time.Date(now.Year(), now.Month(), now.Day(), cleanupHour, 0, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		return next.Sub(now)
	}
	if m.cleanupInterval <= 0 {
		return defaultCleanupInterval
	}
	return m.cleanupInterval
}

func (m *MetricsScheduler) taskContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	go func() {
		select {
		case <-m.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (m *MetricsScheduler) recoverPanic(where string) {
	if r := recover(); r != nil {
		m.logger.Printf("[MetricsScheduler] panic recovered in %s: %v", where, r)
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

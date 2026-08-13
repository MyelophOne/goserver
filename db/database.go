package db

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/myelophone/goserver"
)

type DBQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

type TxOptions = pgx.TxOptions

var (
	IsoLevelReadCommitted  = pgx.ReadCommitted
	IsoLevelSerializable   = pgx.Serializable
	IsoLevelRepeatableRead = pgx.RepeatableRead
)

type DBStats struct {
	LastResetTime     atomic.Pointer[time.Time]
	TotalQueries      atomic.Int64
	SuccessfulQueries atomic.Int64
	FailedQueries     atomic.Int64
	SlowQueries       atomic.Int64
	ActiveConnections atomic.Int64
	PoolAcquired      atomic.Int64
	PoolIdle          atomic.Int64
	TotalConnections  atomic.Int64
}

type DBStatsCollector struct {
	stats DBStats
	mu    sync.RWMutex
}

func NewDBStatsCollector() *DBStatsCollector {
	now := time.Now()
	collector := &DBStatsCollector{}
	collector.stats.LastResetTime.Store(&now)
	return collector
}

func (c *DBStatsCollector) RecordQuery(success bool, duration time.Duration, slowThreshold time.Duration) {
	c.stats.TotalQueries.Add(1)
	if success {
		c.stats.SuccessfulQueries.Add(1)
	} else {
		c.stats.FailedQueries.Add(1)
	}
	if duration > slowThreshold {
		c.stats.SlowQueries.Add(1)
	}
}

func (c *DBStatsCollector) RecordConnection(acquired bool) {
	if acquired {
		c.stats.ActiveConnections.Add(1)
		c.stats.PoolAcquired.Add(1)
	} else {
		c.stats.ActiveConnections.Add(-1)
	}
}

func (c *DBStatsCollector) RecordIdleConnection() {
	c.stats.PoolIdle.Add(1)
}

func (c *DBStatsCollector) GetStats() DBStats {
	return DBStats{
		TotalQueries:      atomic.Int64{},
		SuccessfulQueries: atomic.Int64{},
		FailedQueries:     atomic.Int64{},
		SlowQueries:       atomic.Int64{},
		ActiveConnections: atomic.Int64{},
		PoolAcquired:      atomic.Int64{},
		PoolIdle:          atomic.Int64{},
		TotalConnections:  atomic.Int64{},
		LastResetTime:     atomic.Pointer[time.Time]{},
	}
}

func (c *DBStatsCollector) Reset() {
	now := time.Now()
	c.stats.TotalQueries.Store(0)
	c.stats.SuccessfulQueries.Store(0)
	c.stats.FailedQueries.Store(0)
	c.stats.SlowQueries.Store(0)
	c.stats.ActiveConnections.Store(0)
	c.stats.PoolAcquired.Store(0)
	c.stats.PoolIdle.Store(0)
	c.stats.TotalConnections.Store(0)
	c.stats.LastResetTime.Store(&now)
}

type Database struct {
	pool   *pgxpool.Pool
	stats  *DBStatsCollector
	tracer *dbTracer
	config *pgxpool.Config
	server *goserver.Server
	mu     sync.RWMutex
}

func NewDatabase(s *goserver.Server) (*Database, error) {
	db := &Database{
		stats:  NewDBStatsCollector(),
		server: s,
	}

	pool, err := ConnectDB(s)
	if err != nil {
		return nil, err
	}

	db.pool = pool
	return db, nil
}

func (db *Database) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

func (db *Database) GetPool() *pgxpool.Pool {
	return db.pool
}

func (db *Database) GetStats() DBStats {
	return db.stats.GetStats()
}

func (db *Database) ResetStats() {
	db.stats.Reset()
}

func ConnectDB(s *goserver.Server) (pool *pgxpool.Pool, err error) {
	dsn := s.Config.DatabaseUrl
	if dsn == "" {
		host := s.Config.PostgresHost
		if host == "" {
			s.Logger.Println("Skipping DB connection...")
			return nil, nil
		}

		dsn = fmt.Sprintf("postgres://%s:%s@%s:5432/%s",
			s.Config.PostgresUser,
			s.Config.PostgresPassword,
			host,
			s.Config.PostgresDb,
		)

		params := "?sslmode=disable"

		if mode := s.Config.DbExecMode; mode != "" {
			params += "&default_query_exec_mode=" + mode
		}

		params += "&pool_max_conns=100"
		params += "&pool_min_conns=10"
		params += "&pool_max_conn_lifetime=1h"
		params += "&pool_max_conn_idle_time=30m"
		params += "&pool_health_check_period=1m"

		params += "&pool_prepared_statements=false"

		dsn += params
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		errMsg := fmt.Errorf("invalid db config: %w", err)
		return nil, errMsg
	}

	maxConns := int32(runtime.NumCPU() * 8)
	if envMax := s.Config.DbMaxConns; envMax != "" {
		if v, err := strconv.Atoi(envMax); err == nil {
			maxConns = int32(v)
		}
	}

	config.MaxConns = maxConns
	config.MinConns = int32(float64(maxConns) * 0.5)

	jitter := time.Duration(rand.Intn(600)) * time.Second
	config.MaxConnLifetime = 2*time.Hour + jitter
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second

	config.ConnConfig.ConnectTimeout = 3 * time.Second
	config.ConnConfig.RuntimeParams = map[string]string{
		"statement_timeout":                   "30000",
		"idle_in_transaction_session_timeout": "10000",
	}

	config.AfterRelease = func(c *pgx.Conn) bool {
		return true
	}

	config.BeforeClose = func(c *pgx.Conn) {
	}

	logMode := s.Config.DbLogMode
	if logMode != "off" {
		config.ConnConfig.Tracer = &dbTracer{
			logger:        s.Logger,
			slowThreshold: 100 * time.Millisecond,
			mode:          logMode,
			stats:         NewDBStatsCollector(),
		}
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		pool, err = pgxpool.NewWithConfig(context.Background(), config)
		if err == nil {
			break
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	if err != nil {
		errMsg := fmt.Errorf("failed to create db pool after retries: %w", lastErr)
		return nil, errMsg
	}

	if s != nil {
		goserver.RegisterOnShutdown(func() {
			s.Logger.Println("gracefully closing DB connection pool...")
			pool.Close()
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		errMsg := fmt.Errorf("database unreachable: %w", err)
		return nil, errMsg
	}

	s.Logger.Printf("connected to Postgres (MaxConns: %d, MinConns: %d, LogMode: %s)",
		maxConns, config.MinConns, logMode)
	return pool, nil
}

func HealthCheckDB(ctx context.Context, s *goserver.Server, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database not connected")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("db ping failed: %w", err)
	}

	stats := pool.Stat()

	acquired := stats.AcquiredConns()
	maxConns := stats.MaxConns()

	if acquired >= maxConns {
		s.Logger.Printf("WARN: DB Pool saturated: %d/%d", acquired, maxConns)
		return fmt.Errorf("db pool saturated: %d/%d connections", acquired, maxConns)
	}

	idle := stats.IdleConns()
	if idle == 0 && acquired > 0 {
		s.Logger.Printf("WARN: No idle connections available")
	}

	readCtx, cancel2 := context.WithTimeout(ctx, 2*time.Second)
	defer cancel2()

	var one int
	if err := pool.QueryRow(readCtx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("db read check failed: %w", err)
	}

	txCtx, cancel3 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel3()

	err := RunInTx(txCtx, s, pool, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	}, func(tx pgx.Tx) error {
		var testValue int
		return tx.QueryRow(txCtx, "SELECT 1").Scan(&testValue)
	})

	if err != nil {
		return fmt.Errorf("db transaction check failed: %w", err)
	}

	return nil
}

type traceKey int

const (
	traceStartKey traceKey = 1
	traceSqlKey   traceKey = 2
	traceArgsKey  traceKey = 3
)

type loggerInterface interface {
	Printf(format string, v ...any)
}

type dbTracer struct {
	logger        loggerInterface
	stats         *DBStatsCollector
	mode          string
	slowThreshold time.Duration
}

func (t *dbTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = context.WithValue(ctx, traceStartKey, time.Now())

	if t.mode != "blind" {
		ctx = context.WithValue(ctx, traceSqlKey, data.SQL)
		if t.mode == "full" {
			ctx = context.WithValue(ctx, traceArgsKey, data.Args)
		}
	}

	return ctx
}

func (t *dbTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(traceStartKey).(time.Time)
	if !ok {
		return
	}

	duration := time.Since(start)
	isSlow := duration > t.slowThreshold
	isError := data.Err != nil

	if t.stats != nil {
		t.stats.RecordQuery(!isError, duration, t.slowThreshold)
	}

	if !isSlow && !isError {
		return
	}

	sqlQuery, _ := ctx.Value(traceSqlKey).(string)
	args, _ := ctx.Value(traceArgsKey).([]any)

	var msg string

	switch t.mode {
	case "blind":
		msg = fmt.Sprintf("Time: %v | SQL: [HIDDEN]", duration)
	case "full":
		msg = fmt.Sprintf("Time: %v | SQL: %s | Args: %v", duration, sqlQuery, args)
	default:
		msg = fmt.Sprintf("Time: %v | SQL: %s", duration, sqlQuery)
	}

	if isError {
		t.logger.Printf("[SQL ERROR] %s | Err: %v", msg, data.Err)
	} else {
		t.logger.Printf("[SQL SLOW] %s", msg)
	}
}

func RunInTx(ctx context.Context, s *goserver.Server, pool *pgxpool.Pool, opts TxOptions, fn func(tx pgx.Tx) error) (err error) {
	if pool == nil {
		return fmt.Errorf("database not initialized")
	}

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		tx, beginErr := pool.BeginTx(ctx, opts)
		if beginErr != nil {
			if attempt == maxRetries {
				return fmt.Errorf("begin tx failed after %d attempts: %w", maxRetries, beginErr)
			}
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
			continue
		}

		err = func() error {
			defer func() {
				if p := recover(); p != nil {
					_ = tx.Rollback(ctx)
					panic(p)
				} else if err != nil {
					_ = tx.Rollback(ctx)
				} else {
					commitErr := tx.Commit(ctx)
					if commitErr != nil {
						err = fmt.Errorf("commit failed: %w", commitErr)
					}
				}
			}()

			return fn(tx)
		}()

		if err != nil && isRetryableError(err) && attempt < maxRetries {
			s.Logger.Printf("Retryable error in transaction (attempt %d/%d): %v", attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
			continue
		}

		break
	}

	return err
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return contains(errStr, "serialization_failure") ||
		contains(errStr, "deadlock_detected") ||
		contains(errStr, "lock_not_available") ||
		contains(errStr, "connection_failure") ||
		contains(errStr, "could not serialize")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func checkDB(q DBQuerier) error {
	if q == nil || (reflect.ValueOf(q).Kind() == reflect.Pointer && reflect.ValueOf(q).IsNil()) {
		return fmt.Errorf("database querier is nil")
	}
	return nil
}

func Select[T any](ctx context.Context, q DBQuerier, sql string, args ...any) ([]T, error) {
	if err := checkDB(q); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, pgx.RowToStructByName[T])
}

func Get[T any](ctx context.Context, q DBQuerier, sql string, args ...any) (T, error) {
	var result T
	if err := checkDB(q); err != nil {
		return result, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
}

func Exec(ctx context.Context, q DBQuerier, sql string, args ...any) (int64, error) {
	if err := checkDB(q); err != nil {
		return 0, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func SendBatch(ctx context.Context, q DBQuerier, batch *pgx.Batch) error {
	if err := checkDB(q); err != nil {
		return err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

	br := q.SendBatch(ctx, batch)
	defer func() {
		_ = br.Close()
	}()

	for i := 0; i < batch.Len(); i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("batch item %d failed: %w", i, err)
		}
	}
	return nil
}

type PoolMonitor struct {
	pool       *pgxpool.Pool
	stats      *DBStatsCollector
	server     *goserver.Server
	stopChan   chan struct{}
	monitoring bool
	mu         sync.RWMutex
}

func NewPoolMonitor(pool *pgxpool.Pool, stats *DBStatsCollector, server *goserver.Server) *PoolMonitor {
	return &PoolMonitor{
		pool:     pool,
		stats:    stats,
		server:   server,
		stopChan: make(chan struct{}),
	}
}

func (m *PoolMonitor) Start() {
	m.mu.Lock()
	if m.monitoring {
		m.mu.Unlock()
		return
	}
	m.monitoring = true
	m.mu.Unlock()

	go m.monitorLoop()
}

func (m *PoolMonitor) Stop() {
	m.mu.Lock()
	if !m.monitoring {
		m.mu.Unlock()
		return
	}
	m.monitoring = false
	m.mu.Unlock()

	close(m.stopChan)
}

func (m *PoolMonitor) monitorLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkPoolHealth()
		case <-m.stopChan:
			return
		}
	}
}

func (m *PoolMonitor) checkPoolHealth() {
	if m.pool == nil {
		return
	}

	stats := m.pool.Stat()

	m.stats.stats.ActiveConnections.Store(int64(stats.AcquiredConns()))
	m.stats.stats.PoolIdle.Store(int64(stats.IdleConns()))
	m.stats.stats.TotalConnections.Store(int64(stats.TotalConns()))

	utilization := float64(stats.AcquiredConns()) / float64(stats.MaxConns())
	if utilization > 0.8 {
		m.server.Logger.Printf("WARN: High pool utilization: %.1f%% (%d/%d)",
			utilization*100, stats.AcquiredConns(), stats.MaxConns())
	}
}

func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return contains(errStr, "connection") ||
		contains(errStr, "timeout") ||
		contains(errStr, "network") ||
		contains(errStr, "refused")
}

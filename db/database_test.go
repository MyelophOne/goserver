package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockLogger struct {
	messages []string
	mu       sync.Mutex
}

func (m *mockLogger) Printf(format string, v ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, fmt.Sprintf(format, v...))
}

func (m *mockLogger) Println(v ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, fmt.Sprint(v...))
}

func (m *mockLogger) GetMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.messages...)
}

func (m *mockLogger) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
}

type TestUser struct {
	ID    int    `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func TestDBStatsCollector(t *testing.T) {
	t.Run("NewDBStatsCollector", func(t *testing.T) {
		collector := NewDBStatsCollector()
		if collector == nil {
			t.Fatal("Failed to create stats collector")
		}
	})

	t.Run("RecordQuery", func(t *testing.T) {
		collector := NewDBStatsCollector()

		collector.RecordQuery(true, 50*time.Millisecond, 100*time.Millisecond)

		collector.RecordQuery(false, 50*time.Millisecond, 100*time.Millisecond)

		collector.RecordQuery(true, 150*time.Millisecond, 100*time.Millisecond)

		if collector.stats.TotalQueries.Load() != 3 {
			t.Errorf("Expected 3 total queries, got %d", collector.stats.TotalQueries.Load())
		}
		if collector.stats.SuccessfulQueries.Load() != 2 {
			t.Errorf("Expected 2 successful queries, got %d", collector.stats.SuccessfulQueries.Load())
		}
		if collector.stats.FailedQueries.Load() != 1 {
			t.Errorf("Expected 1 failed query, got %d", collector.stats.FailedQueries.Load())
		}
		if collector.stats.SlowQueries.Load() != 1 {
			t.Errorf("Expected 1 slow query, got %d", collector.stats.SlowQueries.Load())
		}
	})

	t.Run("RecordConnection", func(t *testing.T) {
		collector := NewDBStatsCollector()

		collector.RecordConnection(true)
		if collector.stats.ActiveConnections.Load() != 1 {
			t.Errorf("Expected 1 active connection, got %d", collector.stats.ActiveConnections.Load())
		}
		if collector.stats.PoolAcquired.Load() != 1 {
			t.Errorf("Expected 1 pool acquired, got %d", collector.stats.PoolAcquired.Load())
		}

		collector.RecordConnection(false)
		if collector.stats.ActiveConnections.Load() != 0 {
			t.Errorf("Expected 0 active connections, got %d", collector.stats.ActiveConnections.Load())
		}
	})

	t.Run("RecordIdleConnection", func(t *testing.T) {
		collector := NewDBStatsCollector()

		collector.RecordIdleConnection()
		if collector.stats.PoolIdle.Load() != 1 {
			t.Errorf("Expected 1 idle connection, got %d", collector.stats.PoolIdle.Load())
		}
	})

	t.Run("Reset", func(t *testing.T) {
		collector := NewDBStatsCollector()

		collector.RecordQuery(true, 50*time.Millisecond, 100*time.Millisecond)
		collector.RecordConnection(true)

		collector.Reset()

		if collector.stats.TotalQueries.Load() != 0 {
			t.Errorf("Expected 0 queries after reset, got %d", collector.stats.TotalQueries.Load())
		}
		if collector.stats.ActiveConnections.Load() != 0 {
			t.Errorf("Expected 0 active connections after reset, got %d", collector.stats.ActiveConnections.Load())
		}

		resetTime := collector.stats.LastResetTime.Load()
		if resetTime == nil {
			t.Error("LastResetTime should not be nil after reset")
		}
	})

	t.Run("GetStats", func(t *testing.T) {
		collector := NewDBStatsCollector()

		stats := collector.GetStats()
		if stats.TotalQueries.Load() != 0 {
			t.Error("GetStats should return new struct with zero values")
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("contains", func(t *testing.T) {
		if !contains("hello world", "hello") {
			t.Error("contains should find substring at beginning")
		}
		if !contains("hello world", "world") {
			t.Error("contains should find substring at end")
		}
		if !contains("hello world", "lo wo") {
			t.Error("contains should find substring in middle")
		}
		if contains("hello", "hello world") {
			t.Error("contains should not find longer substring")
		}
		if contains("", "test") {
			t.Error("contains should return false for empty string")
		}
	})

	t.Run("isRetryableError", func(t *testing.T) {
		testCases := []struct {
			name     string
			err      error
			expected bool
		}{
			{"serialization_failure", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40001"}), true},
			{"deadlock_detected", &pgconn.PgError{Code: "40P01"}, true},
			{"lock_not_available", &pgconn.PgError{Code: "55P03"}, false},
			{"connection_failure", &pgconn.PgError{Code: "08006"}, false},
			{"could not serialize", fmt.Errorf("could not serialize"), false},
			{"normal error", fmt.Errorf("normal error"), false},
			{"nil error", nil, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := isRetryableError(tc.err)
				if result != tc.expected {
					t.Errorf("isRetryableError(%v) = %v, expected %v", tc.err, result, tc.expected)
				}
			})
		}
	})

	t.Run("IsConnectionError", func(t *testing.T) {
		testCases := []struct {
			name     string
			err      error
			expected bool
		}{
			{"connection refused", fmt.Errorf("connection refused"), true},
			{"timeout", fmt.Errorf("timeout"), true},
			{"network unreachable", fmt.Errorf("network unreachable"), true},
			{"normal error", fmt.Errorf("normal error"), false},
			{"nil error", nil, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := IsConnectionError(tc.err)
				if result != tc.expected {
					t.Errorf("IsConnectionError(%v) = %v, expected %v", tc.err, result, tc.expected)
				}
			})
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		ctx := context.Background()

		timeoutCtx, cancel := WithTimeout(ctx, 100*time.Millisecond)
		if timeoutCtx == nil {
			t.Fatal("WithTimeout returned nil context")
		}

		if _, hasDeadline := timeoutCtx.Deadline(); !hasDeadline {
			t.Error("WithTimeout should set deadline")
		}
		cancel()

		existingCtx, existingCancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer existingCancel()

		timeoutCtx2, cancel2 := WithTimeout(existingCtx, 50*time.Millisecond)
		if timeoutCtx2 != existingCtx {
			t.Error("WithTimeout should return original context when it already has deadline")
		}
		cancel2()
	})
}

func TestDbTracer(t *testing.T) {
	logger := &mockLogger{}
	statsCollector := NewDBStatsCollector()

	tracer := &dbTracer{
		logger:        logger,
		slowThreshold: 100 * time.Millisecond,
		mode:          "sanitized",
		stats:         statsCollector,
	}

	ctx := context.Background()
	data := pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM users",
		Args: []any{1},
	}

	ctx = tracer.TraceQueryStart(ctx, nil, data)

	if startTime, ok := ctx.Value(traceStartKey).(time.Time); !ok || startTime.IsZero() {
		t.Error("TraceQueryStart should set start time in context")
	}

	if sql, ok := ctx.Value(traceSqlKey).(string); !ok || sql != "SELECT * FROM users" {
		t.Error("TraceQueryStart should set SQL in context for sanitized mode")
	}

	if _, ok := ctx.Value(traceArgsKey).([]any); ok {
		t.Error("Args should not be saved in sanitized mode")
	}

	t.Run("Full mode", func(t *testing.T) {
		fullTracer := &dbTracer{
			logger:        logger,
			slowThreshold: 100 * time.Millisecond,
			mode:          "full",
			stats:         statsCollector,
		}

		ctx2 := fullTracer.TraceQueryStart(context.Background(), nil, data)
		if args, ok := ctx2.Value(traceArgsKey).([]any); !ok || len(args) != 1 {
			t.Error("Args should be saved in full mode")
		}
	})

	t.Run("Blind mode", func(t *testing.T) {
		blindTracer := &dbTracer{
			logger:        logger,
			slowThreshold: 100 * time.Millisecond,
			mode:          "blind",
			stats:         statsCollector,
		}

		ctx3 := blindTracer.TraceQueryStart(context.Background(), nil, data)
		if _, ok := ctx3.Value(traceSqlKey).(string); ok {
			t.Error("SQL should not be saved in blind mode")
		}
	})

	t.Run("Fast successful query", func(t *testing.T) {
		logger.Clear()

		endData := pgx.TraceQueryEndData{Err: nil}
		tracer.TraceQueryEnd(ctx, nil, endData)

		messages := logger.GetMessages()
		if len(messages) > 0 {
			t.Error("Fast successful queries should not be logged")
		}
	})

	t.Run("Slow query", func(t *testing.T) {
		logger.Clear()

		pastTime := time.Now().Add(-200 * time.Millisecond)
		slowCtx := context.WithValue(ctx, traceStartKey, pastTime)

		endData := pgx.TraceQueryEndData{Err: nil}
		tracer.TraceQueryEnd(slowCtx, nil, endData)

		messages := logger.GetMessages()
		if len(messages) == 0 {
			t.Error("Slow queries should be logged")
		}

		msg := messages[0]
		if !contains(msg, "SELECT * FROM users") {
			t.Error("Slow query log should contain SQL")
		}

		if statsCollector.stats.SlowQueries.Load() == 0 {
			t.Error("Slow query should be recorded in stats")
		}
	})

	t.Run("Error query", func(t *testing.T) {
		logger.Clear()

		endData := pgx.TraceQueryEndData{Err: fmt.Errorf("query failed")}
		tracer.TraceQueryEnd(ctx, nil, endData)

		messages := logger.GetMessages()
		if len(messages) == 0 {
			t.Error("Error queries should be logged")
		}

		msg := messages[0]
		if !contains(msg, "query failed") {
			t.Error("Error query log should contain error message")
		}

		if statsCollector.stats.FailedQueries.Load() == 0 {
			t.Error("Failed query should be recorded in stats")
		}
	})
}

func TestIntegrationScenarios(t *testing.T) {
	t.Run("Concurrent stats collection", func(t *testing.T) {
		collector := NewDBStatsCollector()
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					collector.RecordQuery(true, time.Duration(idx+j)*time.Millisecond, 50*time.Millisecond)
					collector.RecordConnection(true)
					collector.RecordConnection(false)
				}
			}(i)
		}

		wg.Wait()

		total := collector.stats.TotalQueries.Load()
		if total != 1000 {
			t.Errorf("Expected 1000 total queries, got %d", total)
		}
	})
}

func BenchmarkDBStatsCollector(b *testing.B) {
	collector := NewDBStatsCollector()

	b.Run("RecordQuery", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordQuery(true, 50*time.Millisecond, 100*time.Millisecond)
		}
	})

	b.Run("RecordConnection", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordConnection(true)
			collector.RecordConnection(false)
		}
	})

	b.Run("Concurrent recording", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				collector.RecordQuery(true, 50*time.Millisecond, 100*time.Millisecond)
			}
		})
	})
}

func BenchmarkHelperFunctions(b *testing.B) {
	b.Run("contains", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = contains("hello world test string for benchmark", "benchmark")
		}
	})

	b.Run("isRetryableError", func(b *testing.B) {
		err := fmt.Errorf("serialization_failure: could not serialize")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = isRetryableError(err)
		}
	})
}

func ExampleTestUser() {
	var _ = TestUser{
		ID:    1,
		Name:  "Test User",
		Email: "test@example.com",
	}
}

func TestExportedConstants(t *testing.T) {
	var _ TxOptions
	var _ = IsoLevelReadCommitted
	var _ = IsoLevelSerializable
	var _ = IsoLevelRepeatableRead

	var _ loggerInterface = &mockLogger{}
}

func TestEdgeCases(t *testing.T) {
	t.Run("Empty string in contains", func(t *testing.T) {
		if !contains("", "") {
			t.Error("contains should return true for empty string in empty string")
		}
	})

	t.Run("Nil error checks", func(t *testing.T) {
		if isRetryableError(nil) {
			t.Error("isRetryableError should return false for nil error")
		}
		if IsConnectionError(nil) {
			t.Error("IsConnectionError should return false for nil error")
		}
	})

	t.Run("Context with existing deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		newCtx, newCancel := WithTimeout(ctx, 100*time.Millisecond)
		if newCtx != ctx {
			t.Error("WithTimeout should return same context when it has deadline")
		}
		newCancel()
	})
}

func TestAtomicCounters(t *testing.T) {
	collector := NewDBStatsCollector()

	if collector.stats.TotalQueries.Load() != 0 {
		t.Error("Initial total queries should be 0")
	}

	collector.stats.TotalQueries.Add(5)
	if collector.stats.TotalQueries.Load() != 5 {
		t.Error("Total queries should be 5 after adding")
	}

	collector.stats.TotalQueries.Add(-2)
	if collector.stats.TotalQueries.Load() != 3 {
		t.Error("Total queries should be 3 after subtracting")
	}

	collector.stats.TotalQueries.Store(100)
	if collector.stats.TotalQueries.Load() != 100 {
		t.Error("Total queries should be 100 after store")
	}

	swapped := collector.stats.TotalQueries.CompareAndSwap(100, 200)
	if !swapped {
		t.Error("CompareAndSwap should succeed when expected value matches")
	}
	if collector.stats.TotalQueries.Load() != 200 {
		t.Error("Total queries should be 200 after successful CompareAndSwap")
	}

	swapped = collector.stats.TotalQueries.CompareAndSwap(100, 300)
	if swapped {
		t.Error("CompareAndSwap should fail when expected value doesn't match")
	}
	if collector.stats.TotalQueries.Load() != 200 {
		t.Error("Total queries should still be 200 after failed CompareAndSwap")
	}
}

func TestTimeHandling(t *testing.T) {
	collector := NewDBStatsCollector()

	initialTime := collector.stats.LastResetTime.Load()
	if initialTime == nil {
		t.Fatal("LastResetTime should not be nil initially")
	}

	collector.Reset()

	newTime := collector.stats.LastResetTime.Load()
	if newTime == nil {
		t.Fatal("LastResetTime should not be nil after reset")
	}

	newTimeValue := *newTime
	if newTimeValue.IsZero() {
		t.Error("Reset time should not be zero")
	}
}

func TestLoggerConcurrency(t *testing.T) {
	logger := &mockLogger{}
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				logger.Printf("Message %d from goroutine %d", j, idx)
			}
		}(i)
	}

	wg.Wait()

	messages := logger.GetMessages()
	if len(messages) != 1000 {
		t.Errorf("Expected 1000 messages, got %d", len(messages))
	}

	logger.Clear()
	messages = logger.GetMessages()
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after clear, got %d", len(messages))
	}
}

func TestSlowQueryThresholds(t *testing.T) {
	collector := NewDBStatsCollector()

	durations := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
		150 * time.Millisecond,
		200 * time.Millisecond,
	}

	threshold := 100 * time.Millisecond

	for _, duration := range durations {
		collector.RecordQuery(true, duration, threshold)
	}

	if collector.stats.TotalQueries.Load() != 4 {
		t.Errorf("Expected 4 total queries, got %d", collector.stats.TotalQueries.Load())
	}

	if collector.stats.SlowQueries.Load() != 2 {
		t.Errorf("Expected 2 slow queries, got %d", collector.stats.SlowQueries.Load())
	}
}

func TestEdgeCaseDurations(t *testing.T) {
	collector := NewDBStatsCollector()

	collector.RecordQuery(true, 0, 100*time.Millisecond)
	collector.RecordQuery(true, -50*time.Millisecond, 100*time.Millisecond)

	collector.RecordQuery(true, 10*time.Second, 100*time.Millisecond)

	if collector.stats.TotalQueries.Load() != 3 {
		t.Errorf("Expected 3 total queries, got %d", collector.stats.TotalQueries.Load())
	}

	if collector.stats.SlowQueries.Load() != 1 {
		t.Errorf("Expected 1 slow query, got %d", collector.stats.SlowQueries.Load())
	}
}

func TestErrorTypes(t *testing.T) {
	testCases := []struct {
		name         string
		errMsg       string
		isRetryable  bool
		isConnection bool
	}{
		{
			name:         "serialization_failure",
			errMsg:       "serialization_failure: could not serialize access due to concurrent update",
			isRetryable:  false,
			isConnection: false,
		},
		{
			name:         "deadlock_detected",
			errMsg:       "deadlock_detected",
			isRetryable:  false,
			isConnection: false,
		},
		{
			name:         "connection_refused",
			errMsg:       "connection refused",
			isRetryable:  false,
			isConnection: true,
		},
		{
			name:         "network_error",
			errMsg:       "network is unreachable",
			isRetryable:  false,
			isConnection: true,
		},
		{
			name:         "timeout",
			errMsg:       "timeout",
			isRetryable:  false,
			isConnection: true,
		},
		{
			name:         "normal_error",
			errMsg:       "some normal error",
			isRetryable:  false,
			isConnection: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tc.errMsg)

			retryable := isRetryableError(err)
			if retryable != tc.isRetryable {
				t.Errorf("isRetryableError(%s) = %v, expected %v", tc.errMsg, retryable, tc.isRetryable)
			}

			connection := IsConnectionError(err)
			if connection != tc.isConnection {
				t.Errorf("IsConnectionError(%s) = %v, expected %v", tc.errMsg, connection, tc.isConnection)
			}
		})
	}
}

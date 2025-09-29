package cache

import (
	"context"
	"go.uber.org/zap/zapcore"
	"testing"
	"time"
)

func TestCacheConfig(t *testing.T) {
	// 测试MySQL配置
	mysqlConfig := &CacheConfig{
		Type:          "mysql",
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
		MaxCacheSize:  10000,
		Async:         true,
		MySQL: &MySQLConfig{
			Host:         "localhost",
			Port:         3306,
			Username:     "root",
			Password:     "password",
			Database:     "zlog_test",
			Charset:      "utf8mb4",
			MaxOpenConns: 100,
			MaxIdleConns: 10,
			TablePrefix:  "zlog",
		},
	}

	if mysqlConfig.Type != "mysql" {
		t.Error("缓存类型应该是mysql")
	}

	if mysqlConfig.MySQL.Host != "localhost" {
		t.Error("MySQL主机配置错误")
	}

	// 测试Redis配置
	redisConfig := &CacheConfig{
		Type:          "redis",
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
		MaxCacheSize:  10000,
		Async:         true,
		Redis: &RedisConfig{
			Host:      "localhost",
			Port:      6379,
			Password:  "",
			Database:  0,
			PoolSize:  10,
			KeyPrefix: "zlog",
		},
	}

	if redisConfig.Type != "redis" {
		t.Error("缓存类型应该是redis")
	}

	if redisConfig.Redis.Host != "localhost" {
		t.Error("Redis主机配置错误")
	}
}

func TestLogEntry(t *testing.T) {
	entry := LogEntry{
		ID:        1,
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "测试日志",
		Logger:    "test",
		Caller:    "test.go:10",
		Project:   "test-project",
		Fields: map[string]interface{}{
			"user_id": 12345,
			"action":  "login",
		},
		CreatedAt: time.Now(),
	}

	if entry.Level != "info" {
		t.Error("日志级别应该是info")
	}

	if entry.Message != "测试日志" {
		t.Error("日志消息错误")
	}

	if len(entry.Fields) != 2 {
		t.Error("字段数量错误")
	}
}

func TestQueryOptions(t *testing.T) {
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()

	opts := &QueryOptions{
		StartTime: &startTime,
		EndTime:   &endTime,
		Levels:    []string{"info", "error"},
		Projects:  []string{"user-service"},
		Keywords:  []string{"登录", "失败"},
		Limit:     100,
		Offset:    0,
		OrderBy:   "timestamp",
		OrderDir:  "desc",
	}

	if opts.Limit != 100 {
		t.Error("查询限制应该是100")
	}

	if len(opts.Levels) != 2 {
		t.Error("级别数量应该是2")
	}

	if opts.OrderBy != "timestamp" {
		t.Error("排序字段应该是timestamp")
	}
}

func TestCacheFactory(t *testing.T) {
	factory := NewCacheFactory()

	// 测试MySQL缓存创建（需要真实的MySQL实例）
	mysqlConfig := &CacheConfig{
		Type: "mysql",
		MySQL: &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "password",
			Database: "zlog_test",
		},
	}

	// 注意：这个测试需要真实的MySQL实例，如果没有会失败
	mysqlCache, err := factory.CreateCache(mysqlConfig)
	if err != nil {
		t.Logf("MySQL缓存创建失败（预期的，如果没有MySQL实例）: %v", err)
	} else {
		defer mysqlCache.Close()
		t.Log("MySQL缓存创建成功")
	}

	// 测试Redis缓存创建（需要真实的Redis实例）
	redisConfig := &CacheConfig{
		Type: "redis",
		Redis: &RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
	}

	// 注意：这个测试需要真实的Redis实例，如果没有会失败
	redisCache, err := factory.CreateCache(redisConfig)
	if err != nil {
		t.Logf("Redis缓存创建失败（预期的，如果没有Redis实例）: %v", err)
	} else {
		defer redisCache.Close()
		t.Log("Redis缓存创建成功")
	}

	// 测试不支持的缓存类型
	invalidConfig := &CacheConfig{
		Type: "invalid",
	}

	_, err = factory.CreateCache(invalidConfig)
	if err == nil {
		t.Error("应该返回错误，因为不支持invalid类型")
	}
}

func TestMultiCacheWriter(t *testing.T) {
	// 创建模拟的写入器
	writers := make([]zapcore.WriteSyncer, 0)

	// 添加一些模拟写入器（这里只是测试接口，不实际写入）
	writer1 := &mockWriteSyncer{name: "writer1"}
	writer2 := &mockWriteSyncer{name: "writer2"}

	writers = append(writers, writer1, writer2)

	multiWriter := NewMultiCacheWriter(writers...)

	// 测试写入
	testData := []byte("test log message")
	n, err := multiWriter.Write(testData)
	if err != nil {
		t.Errorf("写入失败: %v", err)
	}

	if n != len(testData) {
		t.Errorf("写入字节数错误: 期望%d, 实际%d", len(testData), n)
	}

	// 测试同步
	if err := multiWriter.Sync(); err != nil {
		t.Errorf("同步失败: %v", err)
	}

	// 测试添加和移除写入器
	writer3 := &mockWriteSyncer{name: "writer3"}
	multiWriter.AddWriter(writer3)
	multiWriter.RemoveWriter(writer1)

	// 测试WriteBatch方法
	mockCache1 := &mockCache{}
	mockCache2 := &mockCache{}

	multiWriter.AddCache(mockCache1)
	multiWriter.AddCache(mockCache2)

	testEntries := []LogEntry{
		{
			ID:        1,
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "测试批量写入",
			Logger:    "test",
			Project:   "test-project",
			CreatedAt: time.Now(),
		},
	}

	if err := multiWriter.WriteBatch(context.Background(), testEntries); err != nil {
		t.Errorf("批量写入失败: %v", err)
	}
}

func TestCacheQueryOptimizer(t *testing.T) {
	// 创建模拟缓存
	mockCache := &mockCache{}

	optimizer := NewCacheQueryOptimizer(mockCache)

	// 测试查询
	opts := &QueryOptions{
		Limit:  10,
		Offset: 0,
	}

	result, err := optimizer.OptimizedQuery(context.Background(), opts)
	if err != nil {
		t.Errorf("查询失败: %v", err)
	}

	if result == nil {
		t.Error("查询结果不应该为nil")
	}

	// 测试缓存清理
	optimizer.ClearCache()
}

func TestCacheMonitor(t *testing.T) {
	monitor := NewCacheMonitor()

	// 记录一些操作
	for i := 0; i < 100; i++ {
		hit := i%2 == 0 // 50%命中率
		monitor.RecordQuery(hit)

		success := i%10 != 0 // 90%成功率
		monitor.RecordWrite(success)
	}

	// 获取统计信息
	stats := monitor.GetStats()
	hitRate := monitor.GetCacheHitRate()

	if stats.TotalQueries != 100 {
		t.Errorf("总查询数错误: 期望100, 实际%d", stats.TotalQueries)
	}

	if stats.TotalWrites != 100 {
		t.Errorf("总写入数错误: 期望100, 实际%d", stats.TotalWrites)
	}

	expectedHitRate := 50.0
	if hitRate != expectedHitRate {
		t.Errorf("命中率错误: 期望%.1f%%, 实际%.1f%%", expectedHitRate, hitRate)
	}

	// 测试重置统计
	monitor.ResetStats()
	stats = monitor.GetStats()
	if stats.TotalQueries != 0 {
		t.Error("重置后总查询数应该为0")
	}
}

// 模拟写入同步器
type mockWriteSyncer struct {
	name string
}

func (m *mockWriteSyncer) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockWriteSyncer) Sync() error {
	return nil
}

// 模拟缓存
type mockCache struct{}

func (m *mockCache) WriteLogs(ctx context.Context, logs ...LogEntry) error {
	return nil
}

func (m *mockCache) WriteBatch(ctx context.Context, logs []LogEntry) error {
	return nil
}

func (m *mockCache) Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error) {
	return &QueryResult{
		Logs:  []LogEntry{},
		Total: 0,
		Page:  1,
		Size:  0,
	}, nil
}

func (m *mockCache) GetByID(ctx context.Context, id int64) (*LogEntry, error) {
	return nil, nil
}

func (m *mockCache) Count(ctx context.Context, opts *QueryOptions) (int64, error) {
	return 0, nil
}

func (m *mockCache) DeleteExpired(ctx context.Context, before time.Time) error {
	return nil
}

func (m *mockCache) Close() error {
	return nil
}

func (m *mockCache) Ping(ctx context.Context) error {
	return nil
}

func (m *mockCache) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockCache) Sync() error {
	return nil
}

// 基准测试
func BenchmarkLogEntryCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		entry := LogEntry{
			ID:        int64(i),
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "benchmark test message",
			Logger:    "benchmark",
			Caller:    "benchmark.go:10",
			Project:   "benchmark-project",
			Fields: map[string]interface{}{
				"iteration": i,
				"timestamp": time.Now().UnixNano(),
			},
			CreatedAt: time.Now(),
		}
		_ = entry
	}
}

func BenchmarkQueryOptionsCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		opts := &QueryOptions{
			StartTime: func() *time.Time { t := time.Now().Add(-1 * time.Hour); return &t }(),
			EndTime:   func() *time.Time { t := time.Now(); return &t }(),
			Levels:    []string{"info", "error", "warn"},
			Projects:  []string{"user-service", "order-service"},
			Keywords:  []string{"登录", "失败", "错误"},
			Limit:     100,
			Offset:    0,
			OrderBy:   "timestamp",
			OrderDir:  "desc",
		}
		_ = opts
	}
}

func BenchmarkCacheMonitor(b *testing.B) {
	monitor := NewCacheMonitor()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hit := i%2 == 0
		monitor.RecordQuery(hit)

		success := i%10 != 0
		monitor.RecordWrite(success)
	}
}

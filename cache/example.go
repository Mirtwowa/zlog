package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
)

// CacheExample 演示如何使用缓存功能
func CacheExample() {
	// 方法1: 通过配置文件初始化
	config := &CacheConfig{}

	// 使用viper读取配置
	v := viper.New()
	v.SetConfigFile("config.yaml")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}

	// 使用mapstructure将配置映射到结构体
	if err := v.Unmarshal(config); err != nil {
		panic(err)
	}

	// 创建缓存实例
	cache, err := CreateCache(config)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	// 写入日志
	entries := []LogEntry{
		{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "用户登录成功",
			Logger:    "auth",
			Caller:    "login.go:25",
			Project:   "user-service",
			Fields: map[string]interface{}{
				"user_id":  12345,
				"username": "john_doe",
				"ip":       "192.168.1.100",
			},
			CreatedAt: time.Now(),
		},
		{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   "数据库连接失败",
			Logger:    "database",
			Caller:    "db.go:45",
			Project:   "user-service",
			Fields: map[string]interface{}{
				"error":       "connection timeout",
				"database":    "user_db",
				"retry_count": 3,
				"duration_ms": 5000,
			},
			CreatedAt: time.Now(),
		},
	}

	// 批量写入
	if err := cache.WriteBatch(context.Background(), entries); err != nil {
		log.Printf("批量写入失败: %v", err)
	}

	// 查询日志
	queryOpts := &QueryOptions{
		StartTime: func() *time.Time { t := time.Now().Add(-1 * time.Hour); return &t }(),
		EndTime:   func() *time.Time { t := time.Now(); return &t }(),
		Levels:    []string{"info", "error"},
		Projects:  []string{"user-service"},
		Limit:     100,
		Offset:    0,
		OrderBy:   "timestamp",
		OrderDir:  "desc",
	}

	result, err := cache.Query(context.Background(), queryOpts)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return
	}

	fmt.Printf("查询结果: 总数=%d, 当前页=%d, 大小=%d\n", result.Total, result.Page, result.Size)
	for _, entry := range result.Logs {
		fmt.Printf("ID=%d, 时间=%s, 级别=%s, 消息=%s, 项目=%s\n",
			entry.ID, entry.Timestamp.Format("2006-01-02 15:04:05"), entry.Level, entry.Message, entry.Project)
	}
}

// CacheExampleDirect 演示如何直接配置缓存
func CacheExampleDirect() {
	// 方法2: 直接配置MySQL缓存
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
			Database:     "zlog",
			Charset:      "utf8mb4",
			MaxOpenConns: 100,
			MaxIdleConns: 10,
			TablePrefix:  "zlog",
		},
	}

	mysqlCache, err := CreateCache(mysqlConfig)
	if err != nil {
		log.Printf("创建MySQL缓存失败: %v", err)
		return
	}
	defer mysqlCache.Close()

	// 方法3: 直接配置Redis缓存
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

	redisCache, err := CreateCache(redisConfig)
	if err != nil {
		log.Printf("创建Redis缓存失败: %v", err)
		return
	}
	defer redisCache.Close()

	// 使用多缓存写入器
	multiWriter := NewMultiCacheWriter()
	multiWriter.AddCache(mysqlCache)
	multiWriter.AddCache(redisCache)

	// 写入测试日志
	testEntry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "多缓存写入测试",
		Logger:    "test",
		Project:   "cache-test",
		Fields: map[string]interface{}{
			"test_id": "multi-cache-test",
			"count":   1,
		},
		CreatedAt: time.Now(),
	}

	if err := multiWriter.WriteBatch(context.Background(), []LogEntry{testEntry}); err != nil {
		log.Printf("多缓存写入失败: %v", err)
	}

	// 同步所有缓存
	if err := multiWriter.Sync(); err != nil {
		log.Printf("同步失败: %v", err)
	}
}

// CacheQueryExample 演示缓存查询功能
func CacheQueryExample() {
	// 创建缓存实例（这里假设已经配置好）
	config := &CacheConfig{
		Type: "mysql",
		MySQL: &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "password",
			Database: "zlog",
		},
	}

	cache, err := CreateCache(config)
	if err != nil {
		log.Printf("创建缓存失败: %v", err)
		return
	}
	defer cache.Close()

	// 创建查询优化器
	optimizer := NewCacheQueryOptimizer(cache)

	// 各种查询示例
	ctx := context.Background()

	// 1. 按时间范围查询
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	timeQuery := &QueryOptions{
		StartTime: &startTime,
		EndTime:   &endTime,
		Limit:     50,
		OrderBy:   "timestamp",
		OrderDir:  "desc",
	}

	result, err := optimizer.OptimizedQuery(ctx, timeQuery)
	if err != nil {
		log.Printf("时间查询失败: %v", err)
	} else {
		fmt.Printf("时间查询结果: %d条记录\n", len(result.Logs))
	}

	// 2. 按级别查询
	levelQuery := &QueryOptions{
		Levels:   []string{"error", "warn"},
		Limit:    100,
		OrderBy:  "timestamp",
		OrderDir: "desc",
	}

	result, err = optimizer.OptimizedQuery(ctx, levelQuery)
	if err != nil {
		log.Printf("级别查询失败: %v", err)
	} else {
		fmt.Printf("级别查询结果: %d条记录\n", len(result.Logs))
	}

	// 3. 按项目查询
	projectQuery := &QueryOptions{
		Projects: []string{"user-service", "order-service"},
		Limit:    50,
		OrderBy:  "timestamp",
		OrderDir: "desc",
	}

	result, err = optimizer.OptimizedQuery(ctx, projectQuery)
	if err != nil {
		log.Printf("项目查询失败: %v", err)
	} else {
		fmt.Printf("项目查询结果: %d条记录\n", len(result.Logs))
	}

	// 4. 关键词搜索
	keywordQuery := &QueryOptions{
		Keywords: []string{"登录", "失败", "错误"},
		Limit:    20,
		OrderBy:  "timestamp",
		OrderDir: "desc",
	}

	result, err = optimizer.OptimizedQuery(ctx, keywordQuery)
	if err != nil {
		log.Printf("关键词查询失败: %v", err)
	} else {
		fmt.Printf("关键词查询结果: %d条记录\n", len(result.Logs))
	}

	// 5. 复合查询
	complexQuery := &QueryOptions{
		StartTime: &startTime,
		EndTime:   &endTime,
		Levels:    []string{"error"},
		Projects:  []string{"user-service"},
		Keywords:  []string{"数据库"},
		Limit:     10,
		OrderBy:   "timestamp",
		OrderDir:  "desc",
	}

	result, err = optimizer.OptimizedQuery(ctx, complexQuery)
	if err != nil {
		log.Printf("复合查询失败: %v", err)
	} else {
		fmt.Printf("复合查询结果: %d条记录\n", len(result.Logs))
	}
}

// CacheMonitorExample 演示缓存监控功能
func CacheMonitorExample() {
	// 创建监控器
	monitor := NewCacheMonitor()

	// 模拟一些操作
	for i := 0; i < 100; i++ {
		// 模拟查询
		hit := i%3 == 0 // 33%命中率
		monitor.RecordQuery(hit)

		// 模拟写入
		success := i%10 != 0 // 90%成功率
		monitor.RecordWrite(success)

		time.Sleep(10 * time.Millisecond)
	}

	// 获取统计信息
	stats := monitor.GetStats()
	hitRate := monitor.GetCacheHitRate()

	fmt.Printf("缓存统计信息:\n")
	fmt.Printf("  总查询数: %d\n", stats.TotalQueries)
	fmt.Printf("  缓存命中: %d\n", stats.CacheHits)
	fmt.Printf("  缓存未命中: %d\n", stats.CacheMisses)
	fmt.Printf("  命中率: %.2f%%\n", hitRate)
	fmt.Printf("  总写入数: %d\n", stats.TotalWrites)
	fmt.Printf("  写入失败: %d\n", stats.FailedWrites)
	fmt.Printf("  最后查询时间: %s\n", stats.LastQueryTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  最后写入时间: %s\n", stats.LastWriteTime.Format("2006-01-02 15:04:05"))
}

// CachePerformanceExample 演示性能测试
func CachePerformanceExample() {
	config := &CacheConfig{
		Type: "mysql",
		MySQL: &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "password",
			Database: "zlog",
		},
	}

	cache, err := CreateCache(config)
	if err != nil {
		log.Printf("创建缓存失败: %v", err)
		return
	}
	defer cache.Close()

	// 性能测试：批量写入
	fmt.Println("开始批量写入性能测试...")
	start := time.Now()

	batchSize := 1000
	entries := make([]LogEntry, batchSize)

	for i := 0; i < batchSize; i++ {
		entries[i] = LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   fmt.Sprintf("性能测试日志 %d", i),
			Logger:    "performance",
			Project:   "perf-test",
			Fields: map[string]interface{}{
				"batch_id":  "perf-test-batch",
				"index":     i,
				"timestamp": time.Now().UnixNano(),
			},
			CreatedAt: time.Now(),
		}
	}

	if err := cache.WriteBatch(context.Background(), entries); err != nil {
		log.Printf("批量写入失败: %v", err)
		return
	}

	duration := time.Since(start)
	fmt.Printf("批量写入 %d 条记录耗时: %v\n", batchSize, duration)
	fmt.Printf("平均每条记录耗时: %v\n", duration/time.Duration(batchSize))
	fmt.Printf("写入速度: %.0f 条/秒\n", float64(batchSize)/duration.Seconds())

	// 性能测试：查询
	fmt.Println("开始查询性能测试...")
	start = time.Now()

	queryOpts := &QueryOptions{
		Projects: []string{"perf-test"},
		Limit:    100,
		OrderBy:  "timestamp",
		OrderDir: "desc",
	}

	result, err := cache.Query(context.Background(), queryOpts)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return
	}

	duration = time.Since(start)
	fmt.Printf("查询 %d 条记录耗时: %v\n", len(result.Logs), duration)
	fmt.Printf("查询速度: %.0f 条/秒\n", float64(len(result.Logs))/duration.Seconds())
}

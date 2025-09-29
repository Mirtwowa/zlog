package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisCache Redis日志缓存实现
type RedisCache struct {
	client *redis.Client
	config *RedisConfig

	// 批量写入相关
	batchBuffer []LogEntry
	batchMutex  sync.Mutex

	// 异步写入相关
	writeChan chan LogEntry
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// 键前缀
	keyPrefix string
}

// NewRedisCache 创建Redis缓存实例
func NewRedisCache(config *RedisConfig) (*RedisCache, error) {
	if config == nil {
		return nil, fmt.Errorf("Redis配置不能为空")
	}

	// 设置默认值
	if config.Port == 0 {
		config.Port = 6379
	}
	if config.PoolSize == 0 {
		config.PoolSize = 10
	}
	if config.KeyPrefix == "" {
		config.KeyPrefix = "zlog"
	}

	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.Database,
		PoolSize: config.PoolSize,
	})

	// 测试连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("Redis连接测试失败: %w", err)
	}

	cache := &RedisCache{
		client:      client,
		config:      config,
		batchBuffer: make([]LogEntry, 0, 1000),
		writeChan:   make(chan LogEntry, 10000),
		keyPrefix:   config.KeyPrefix,
	}

	// 启动异步写入协程
	ctx, cancel := context.WithCancel(context.Background())
	cache.ctx = ctx
	cache.cancel = cancel

	cache.wg.Add(1)
	go cache.asyncWriter()

	return cache, nil
}

// Write 实现zapcore.WriteSyncer接口
func (r *RedisCache) Write(p []byte) (n int, err error) {
	// 解析JSON日志
	var logData map[string]interface{}
	if err := json.Unmarshal(p, &logData); err != nil {
		return len(p), err // 忽略解析错误，避免影响日志输出
	}

	// 转换为LogEntry
	entry := r.parseLogEntry(logData)

	// 异步写入
	select {
	case r.writeChan <- entry:
	default:
		// 如果通道满了，直接丢弃
		log.Printf("Redis写入通道已满，丢弃日志: %s", string(p))
	}

	return len(p), nil
}

// Sync 实现zapcore.WriteSyncer接口
func (r *RedisCache) Sync() error {
	// 刷新批量缓冲区
	r.flushBatch()
	return nil
}

// parseLogEntry 解析日志条目
func (r *RedisCache) parseLogEntry(data map[string]interface{}) LogEntry {
	entry := LogEntry{
		Timestamp: time.Now(),
		CreatedAt: time.Now(),
		Fields:    make(map[string]interface{}),
	}

	// 解析基本字段
	if timestamp, ok := data["time"].(string); ok {
		if t, err := time.Parse("2006-01-02-15:04:05", timestamp); err == nil {
			entry.Timestamp = t
		}
	}

	if level, ok := data["level"].(string); ok {
		entry.Level = level
	}

	if message, ok := data["msg"].(string); ok {
		entry.Message = message
	}

	if logger, ok := data["logger"].(string); ok {
		entry.Logger = logger
	}

	if caller, ok := data["caller"].(string); ok {
		entry.Caller = caller
	}

	if project, ok := data["project"].(string); ok {
		entry.Project = project
	}

	// 解析其他字段
	for key, value := range data {
		if !isReservedField(key) {
			entry.Fields[key] = value
		}
	}

	return entry
}

// asyncWriter 异步写入协程
func (r *RedisCache) asyncWriter() {
	defer r.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			// 处理剩余的日志
			r.flushRemaining()
			return

		case entry := <-r.writeChan:
			r.batchMutex.Lock()
			r.batchBuffer = append(r.batchBuffer, entry)
			if len(r.batchBuffer) >= 100 {
				r.flushBatchUnsafe()
			}
			r.batchMutex.Unlock()

		case <-ticker.C:
			r.flushBatch()
		}
	}
}

// flushBatch 刷新批量缓冲区
func (r *RedisCache) flushBatch() {
	r.batchMutex.Lock()
	defer r.batchMutex.Unlock()
	r.flushBatchUnsafe()
}

// flushBatchUnsafe 不安全的批量刷新（需要在锁内调用）
func (r *RedisCache) flushBatchUnsafe() {
	if len(r.batchBuffer) == 0 {
		return
	}

	if err := r.writeBatchUnsafe(r.batchBuffer); err != nil {
		log.Printf("Redis批量写入失败: %v", err)
	}

	// 清空缓冲区
	r.batchBuffer = r.batchBuffer[:0]
}

// flushRemaining 刷新剩余的日志
func (r *RedisCache) flushRemaining() {
	r.batchMutex.Lock()
	defer r.batchMutex.Unlock()

	// 处理通道中剩余的日志
	for {
		select {
		case entry := <-r.writeChan:
			r.batchBuffer = append(r.batchBuffer, entry)
		default:
			goto flush
		}
	}

flush:
	if len(r.batchBuffer) > 0 {
		r.flushBatchUnsafe()
	}
}

// WriteBatch 批量写入日志
func (r *RedisCache) WriteBatch(ctx context.Context, logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	return r.writeBatchUnsafe(logs)
}

// writeBatchUnsafe 不安全的批量写入
func (r *RedisCache) writeBatchUnsafe(logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()

	for _, entry := range logs {
		// 生成唯一ID
		if entry.ID == 0 {
			entry.ID = time.Now().UnixNano()
		}

		// 构建键
		key := r.buildLogKey(entry)

		// 序列化日志数据
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}

		// 添加到管道
		pipe.Set(r.ctx, key, data, 7*24*time.Hour) // 默认保存7天

		// 添加到索引
		r.addToIndexes(pipe, entry)
	}

	// 执行管道
	_, err := pipe.Exec(r.ctx)
	return err
}

// buildLogKey 构建日志键
func (r *RedisCache) buildLogKey(entry LogEntry) string {
	return fmt.Sprintf("%s:log:%d", r.keyPrefix, entry.ID)
}

// addToIndexes 添加到索引
func (r *RedisCache) addToIndexes(pipe redis.Pipeliner, entry LogEntry) {
	// 时间索引
	timestampKey := fmt.Sprintf("%s:index:timestamp:%s", r.keyPrefix, entry.Timestamp.Format("2006-01-02"))
	pipe.ZAdd(r.ctx, timestampKey, &redis.Z{
		Score:  float64(entry.Timestamp.Unix()),
		Member: entry.ID,
	})
	pipe.Expire(r.ctx, timestampKey, 7*24*time.Hour)

	// 级别索引
	levelKey := fmt.Sprintf("%s:index:level:%s", r.keyPrefix, entry.Level)
	pipe.ZAdd(r.ctx, levelKey, &redis.Z{
		Score:  float64(entry.Timestamp.Unix()),
		Member: entry.ID,
	})
	pipe.Expire(r.ctx, levelKey, 7*24*time.Hour)

	// 项目索引
	if entry.Project != "" {
		projectKey := fmt.Sprintf("%s:index:project:%s", r.keyPrefix, entry.Project)
		pipe.ZAdd(r.ctx, projectKey, &redis.Z{
			Score:  float64(entry.Timestamp.Unix()),
			Member: entry.ID,
		})
		pipe.Expire(r.ctx, projectKey, 7*24*time.Hour)
	}

	// 日志器索引
	if entry.Logger != "" {
		loggerKey := fmt.Sprintf("%s:index:logger:%s", r.keyPrefix, entry.Logger)
		pipe.ZAdd(r.ctx, loggerKey, &redis.Z{
			Score:  float64(entry.Timestamp.Unix()),
			Member: entry.ID,
		})
		pipe.Expire(r.ctx, loggerKey, 7*24*time.Hour)
	}
}

// Query 查询日志
func (r *RedisCache) Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error) {
	if opts == nil {
		opts = &QueryOptions{}
	}

	// 获取匹配的日志ID
	ids, err := r.getMatchingIDs(ctx, opts)
	if err != nil {
		return nil, err
	}

	// 应用分页
	total := int64(len(ids))
	start := 0
	end := len(ids)

	if opts.Offset > 0 && opts.Offset < len(ids) {
		start = opts.Offset
	}

	if opts.Limit > 0 && start+opts.Limit < len(ids) {
		end = start + opts.Limit
	}

	ids = ids[start:end]

	// 批量获取日志数据
	var logs []LogEntry
	if len(ids) > 0 {
		keys := make([]string, len(ids))
		for i, id := range ids {
			keys[i] = fmt.Sprintf("%s:log:%d", r.keyPrefix, id)
		}

		values, err := r.client.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, fmt.Errorf("批量获取日志失败: %w", err)
		}

		for _, value := range values {
			if value == nil {
				continue
			}

			var entry LogEntry
			if err := json.Unmarshal([]byte(value.(string)), &entry); err == nil {
				logs = append(logs, entry)
			}
		}
	}

	return &QueryResult{
		Logs:  logs,
		Total: total,
		Page:  start/opts.Limit + 1,
		Size:  len(logs),
	}, nil
}

// getMatchingIDs 获取匹配的日志ID
func (r *RedisCache) getMatchingIDs(ctx context.Context, opts *QueryOptions) ([]int64, error) {
	var allKeys []string

	// 根据条件构建索引键
	if len(opts.Levels) > 0 {
		for _, level := range opts.Levels {
			allKeys = append(allKeys, fmt.Sprintf("%s:index:level:%s", r.keyPrefix, level))
		}
	} else if len(opts.Projects) > 0 {
		for _, project := range opts.Projects {
			allKeys = append(allKeys, fmt.Sprintf("%s:index:project:%s", r.keyPrefix, project))
		}
	} else if len(opts.Loggers) > 0 {
		for _, logger := range opts.Loggers {
			allKeys = append(allKeys, fmt.Sprintf("%s:index:logger:%s", r.keyPrefix, logger))
		}
	} else {
		// 默认按时间查询
		if opts.StartTime != nil {
			date := opts.StartTime.Format("2006-01-02")
			allKeys = append(allKeys, fmt.Sprintf("%s:index:timestamp:%s", r.keyPrefix, date))
		} else {
			// 查询最近7天
			for i := 0; i < 7; i++ {
				date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
				allKeys = append(allKeys, fmt.Sprintf("%s:index:timestamp:%s", r.keyPrefix, date))
			}
		}
	}

	if len(allKeys) == 0 {
		return []int64{}, nil
	}

	// 合并多个有序集合
	var ids []int64
	if len(allKeys) == 1 {
		// 单个键，直接查询
		results, err := r.client.ZRevRange(ctx, allKeys[0], 0, -1).Result()
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			if id, err := strconv.ParseInt(result, 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	} else {
		// 多个键，使用ZUNIONSTORE合并
		tempKey := fmt.Sprintf("%s:temp:union:%d", r.keyPrefix, time.Now().UnixNano())
		defer r.client.Del(ctx, tempKey)

		err := r.client.ZUnionStore(ctx, tempKey, &redis.ZStore{
			Keys: allKeys,
		}).Err()
		if err != nil {
			return nil, err
		}

		results, err := r.client.ZRevRange(ctx, tempKey, 0, -1).Result()
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			if id, err := strconv.ParseInt(result, 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}

	// 应用时间过滤
	if opts.StartTime != nil || opts.EndTime != nil {
		var filteredIDs []int64
		for _, id := range ids {
			// 从ID中提取时间戳（假设ID是时间戳）
			timestamp := time.Unix(0, id)

			if opts.StartTime != nil && timestamp.Before(*opts.StartTime) {
				continue
			}
			if opts.EndTime != nil && timestamp.After(*opts.EndTime) {
				continue
			}

			filteredIDs = append(filteredIDs, id)
		}
		ids = filteredIDs
	}

	return ids, nil
}

// GetByID 根据ID查询日志
func (r *RedisCache) GetByID(ctx context.Context, id int64) (*LogEntry, error) {
	key := fmt.Sprintf("%s:log:%d", r.keyPrefix, id)

	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("查询日志失败: %w", err)
	}

	var entry LogEntry
	if err := json.Unmarshal([]byte(value), &entry); err != nil {
		return nil, fmt.Errorf("解析日志数据失败: %w", err)
	}

	return &entry, nil
}

// Count 统计日志数量
func (r *RedisCache) Count(ctx context.Context, opts *QueryOptions) (int64, error) {
	ids, err := r.getMatchingIDs(ctx, opts)
	if err != nil {
		return 0, err
	}

	return int64(len(ids)), nil
}

// DeleteExpired 删除过期日志
func (r *RedisCache) DeleteExpired(ctx context.Context, before time.Time) error {
	// Redis使用TTL自动删除过期数据，这里主要是清理索引
	pattern := fmt.Sprintf("%s:index:*", r.keyPrefix)

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("获取索引键失败: %w", err)
	}

	var expiredKeys []string
	for _, key := range keys {
		// 检查键的TTL
		ttl, err := r.client.TTL(ctx, key).Result()
		if err != nil {
			continue
		}

		if ttl < 0 {
			// 没有TTL的键，手动删除
			expiredKeys = append(expiredKeys, key)
		}
	}

	if len(expiredKeys) > 0 {
		return r.client.Del(ctx, expiredKeys...).Err()
	}

	return nil
}

// Ping 健康检查
func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close 关闭连接
func (r *RedisCache) Close() error {
	// 停止异步写入
	r.cancel()
	r.wg.Wait()

	// 关闭通道
	close(r.writeChan)

	// 关闭Redis连接
	return r.client.Close()
}

// WriteLogs 写入日志条目（实现LogCache接口）
func (r *RedisCache) WriteLogs(ctx context.Context, logs ...LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	return r.WriteBatch(ctx, logs)
}

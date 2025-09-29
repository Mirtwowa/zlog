package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// CacheFactory 缓存工厂
type CacheFactory struct {
	caches map[string]LogCache
	mutex  sync.RWMutex
}

// NewCacheFactory 创建缓存工厂
func NewCacheFactory() *CacheFactory {
	return &CacheFactory{
		caches: make(map[string]LogCache),
	}
}

// CreateCache 创建缓存实例
func (f *CacheFactory) CreateCache(config *CacheConfig) (LogCache, error) {
	if config == nil {
		return nil, fmt.Errorf("缓存配置不能为空")
	}

	switch config.Type {
	case "mysql":
		if config.MySQL == nil {
			return nil, fmt.Errorf("MySQL配置不能为空")
		}
		return NewMySQLCache(config.MySQL)

	case "redis":
		if config.Redis == nil {
			return nil, fmt.Errorf("Redis配置不能为空")
		}
		return NewRedisCache(config.Redis)

	default:
		return nil, fmt.Errorf("不支持的缓存类型: %s", config.Type)
	}
}

// GetCache 获取缓存实例
func (f *CacheFactory) GetCache(name string) LogCache {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.caches[name]
}

// RegisterCache 注册缓存实例
func (f *CacheFactory) RegisterCache(name string, cache LogCache) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.caches[name] = cache
}

// UnregisterCache 注销缓存实例
func (f *CacheFactory) UnregisterCache(name string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if cache, exists := f.caches[name]; exists {
		cache.Close()
		delete(f.caches, name)
	}
}

// Close 关闭所有缓存
func (f *CacheFactory) Close() {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	for name, cache := range f.caches {
		if err := cache.Close(); err != nil {
			log.Printf("关闭缓存 %s 失败: %v", name, err)
		}
	}
	f.caches = make(map[string]LogCache)
}

// 全局缓存工厂实例
var globalFactory = NewCacheFactory()

// CreateCache 创建缓存实例（全局函数）
func CreateCache(config *CacheConfig) (LogCache, error) {
	return globalFactory.CreateCache(config)
}

// GetCache 获取缓存实例（全局函数）
func GetCache(name string) LogCache {
	return globalFactory.GetCache(name)
}

// RegisterCache 注册缓存实例（全局函数）
func RegisterCache(name string, cache LogCache) {
	globalFactory.RegisterCache(name, cache)
}

// UnregisterCache 注销缓存实例（全局函数）
func UnregisterCache(name string) {
	globalFactory.UnregisterCache(name)
}

// CloseAllCaches 关闭所有缓存（全局函数）
func CloseAllCaches() {
	globalFactory.Close()
}

// CacheWriteSyncer 创建缓存写入同步器
func CacheWriteSyncer(config *CacheConfig) (zapcore.WriteSyncer, error) {
	cache, err := CreateCache(config)
	if err != nil {
		return nil, err
	}

	if writer, ok := cache.(CacheWriter); ok {
		return writer, nil
	}

	return nil, fmt.Errorf("缓存不支持写入同步器接口")
}

// MultiCacheWriter 多缓存写入器
type MultiCacheWriter struct {
	writers []zapcore.WriteSyncer
	caches  []LogCache
	mutex   sync.RWMutex
}

// NewMultiCacheWriter 创建多缓存写入器
func NewMultiCacheWriter(writers ...zapcore.WriteSyncer) *MultiCacheWriter {
	return &MultiCacheWriter{
		writers: writers,
		caches:  make([]LogCache, 0),
	}
}

// Write 实现zapcore.WriteSyncer接口
func (m *MultiCacheWriter) Write(p []byte) (n int, err error) {
	m.mutex.RLock()
	writers := make([]zapcore.WriteSyncer, len(m.writers))
	copy(writers, m.writers)
	m.mutex.RUnlock()

	// 并发写入到多个缓存
	var wg sync.WaitGroup
	for _, writer := range writers {
		wg.Add(1)
		go func(w zapcore.WriteSyncer) {
			defer wg.Done()
			if _, err := w.Write(p); err != nil {
				log.Printf("多缓存写入失败: %v", err)
			}
		}(writer)
	}

	wg.Wait()
	return len(p), nil
}

// Sync 实现zapcore.WriteSyncer接口
func (m *MultiCacheWriter) Sync() error {
	m.mutex.RLock()
	writers := make([]zapcore.WriteSyncer, len(m.writers))
	copy(writers, m.writers)
	m.mutex.RUnlock()

	var wg sync.WaitGroup
	var errors []error

	for _, writer := range writers {
		wg.Add(1)
		go func(w zapcore.WriteSyncer) {
			defer wg.Done()
			if err := w.Sync(); err != nil {
				errors = append(errors, err)
			}
		}(writer)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("同步失败: %v", errors)
	}

	return nil
}

// AddWriter 添加写入器
func (m *MultiCacheWriter) AddWriter(writer zapcore.WriteSyncer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.writers = append(m.writers, writer)
}

// RemoveWriter 移除写入器
func (m *MultiCacheWriter) RemoveWriter(writer zapcore.WriteSyncer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for i, w := range m.writers {
		if w == writer {
			m.writers = append(m.writers[:i], m.writers[i+1:]...)
			break
		}
	}
}

// WriteBatch 批量写入日志到多个缓存
func (m *MultiCacheWriter) WriteBatch(ctx context.Context, logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	m.mutex.RLock()
	caches := make([]LogCache, len(m.caches))
	copy(caches, m.caches)
	m.mutex.RUnlock()

	// 并发写入到多个缓存
	var wg sync.WaitGroup
	var errors []error
	var errorMutex sync.Mutex

	for _, cache := range caches {
		wg.Add(1)
		go func(c LogCache) {
			defer wg.Done()
			if err := c.WriteBatch(ctx, logs); err != nil {
				errorMutex.Lock()
				errors = append(errors, err)
				errorMutex.Unlock()
			}
		}(cache)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("批量写入失败: %v", errors)
	}

	return nil
}

// AddCache 添加缓存实例
func (m *MultiCacheWriter) AddCache(cache LogCache) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.caches = append(m.caches, cache)
}

// CacheQueryOptimizer 缓存查询优化器
type CacheQueryOptimizer struct {
	cache LogCache
	// 查询缓存
	queryCache map[string]*QueryResult
	cacheMutex sync.RWMutex
	cacheTTL   time.Duration
}

// NewCacheQueryOptimizer 创建查询优化器
func NewCacheQueryOptimizer(cache LogCache) *CacheQueryOptimizer {
	return &CacheQueryOptimizer{
		cache:      cache,
		queryCache: make(map[string]*QueryResult),
		cacheTTL:   5 * time.Minute, // 默认缓存5分钟
	}
}

// OptimizedQuery 优化的查询方法
func (o *CacheQueryOptimizer) OptimizedQuery(ctx context.Context, opts *QueryOptions) (*QueryResult, error) {
	// 生成缓存键
	cacheKey := o.generateCacheKey(opts)

	// 尝试从缓存获取
	o.cacheMutex.RLock()
	if result, exists := o.queryCache[cacheKey]; exists {
		o.cacheMutex.RUnlock()
		return result, nil
	}
	o.cacheMutex.RUnlock()

	// 缓存未命中，执行查询
	result, err := o.cache.Query(ctx, opts)
	if err != nil {
		return nil, err
	}

	// 存储到缓存
	o.cacheMutex.Lock()
	o.queryCache[cacheKey] = result
	o.cacheMutex.Unlock()

	// 异步清理过期缓存
	go o.cleanExpiredCache()

	return result, nil
}

// generateCacheKey 生成缓存键
func (o *CacheQueryOptimizer) generateCacheKey(opts *QueryOptions) string {
	// 这里简化实现，实际应该更复杂的序列化
	return fmt.Sprintf("query_%d_%d_%s", opts.Limit, opts.Offset, opts.OrderBy)
}

// cleanExpiredCache 清理过期缓存
func (o *CacheQueryOptimizer) cleanExpiredCache() {
	// 简化实现，实际应该记录缓存时间
	time.Sleep(o.cacheTTL)

	o.cacheMutex.Lock()
	defer o.cacheMutex.Unlock()

	// 清空所有缓存（简化实现）
	o.queryCache = make(map[string]*QueryResult)
}

// ClearCache 清空查询缓存
func (o *CacheQueryOptimizer) ClearCache() {
	o.cacheMutex.Lock()
	defer o.cacheMutex.Unlock()
	o.queryCache = make(map[string]*QueryResult)
}

// CacheStats 缓存统计信息
type CacheStats struct {
	TotalQueries  int64     `json:"total_queries"`
	CacheHits     int64     `json:"cache_hits"`
	CacheMisses   int64     `json:"cache_misses"`
	TotalWrites   int64     `json:"total_writes"`
	FailedWrites  int64     `json:"failed_writes"`
	LastWriteTime time.Time `json:"last_write_time"`
	LastQueryTime time.Time `json:"last_query_time"`
}

// CacheMonitor 缓存监控器
type CacheMonitor struct {
	stats CacheStats
	mutex sync.RWMutex
}

// NewCacheMonitor 创建缓存监控器
func NewCacheMonitor() *CacheMonitor {
	return &CacheMonitor{
		stats: CacheStats{},
	}
}

// RecordQuery 记录查询
func (m *CacheMonitor) RecordQuery(hit bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.stats.TotalQueries++
	if hit {
		m.stats.CacheHits++
	} else {
		m.stats.CacheMisses++
	}
	m.stats.LastQueryTime = time.Now()
}

// RecordWrite 记录写入
func (m *CacheMonitor) RecordWrite(success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.stats.TotalWrites++
	if !success {
		m.stats.FailedWrites++
	}
	m.stats.LastWriteTime = time.Now()
}

// GetStats 获取统计信息
func (m *CacheMonitor) GetStats() CacheStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.stats
}

// ResetStats 重置统计信息
func (m *CacheMonitor) ResetStats() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.stats = CacheStats{}
}

// GetCacheHitRate 获取缓存命中率
func (m *CacheMonitor) GetCacheHitRate() float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.stats.TotalQueries == 0 {
		return 0
	}

	return float64(m.stats.CacheHits) / float64(m.stats.TotalQueries) * 100
}

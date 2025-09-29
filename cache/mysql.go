package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLCache MySQL日志缓存实现
type MySQLCache struct {
	db     *sql.DB
	config *MySQLConfig

	// 批量写入相关
	batchBuffer []LogEntry
	batchMutex  sync.Mutex

	// 异步写入相关
	writeChan chan LogEntry
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// 表名
	tableName string
}

// NewMySQLCache 创建MySQL缓存实例
func NewMySQLCache(config *MySQLConfig) (*MySQLCache, error) {
	if config == nil {
		return nil, fmt.Errorf("MySQL配置不能为空")
	}

	// 设置默认值
	if config.Port == 0 {
		config.Port = 3306
	}
	if config.Charset == "" {
		config.Charset = "utf8mb4"
	}
	if config.MaxOpenConns == 0 {
		config.MaxOpenConns = 100
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 10
	}
	if config.TablePrefix == "" {
		config.TablePrefix = "zlog"
	}

	// 构建DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		config.Username, config.Password, config.Host, config.Port, config.Database, config.Charset)

	// 连接数据库
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("连接MySQL失败: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("MySQL连接测试失败: %w", err)
	}

	cache := &MySQLCache{
		db:          db,
		config:      config,
		batchBuffer: make([]LogEntry, 0, 1000),
		writeChan:   make(chan LogEntry, 10000),
		tableName:   fmt.Sprintf("%s_logs", config.TablePrefix),
	}

	// 初始化表结构
	if err := cache.initTable(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}

	// 启动异步写入协程
	ctx, cancel := context.WithCancel(context.Background())
	cache.ctx = ctx
	cache.cancel = cancel

	cache.wg.Add(1)
	go cache.asyncWriter()

	return cache, nil
}

// initTable 初始化表结构
func (m *MySQLCache) initTable() error {
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			timestamp DATETIME(3) NOT NULL,
			level VARCHAR(10) NOT NULL,
			message TEXT NOT NULL,
			logger VARCHAR(100) DEFAULT '',
			caller VARCHAR(200) DEFAULT '',
			project VARCHAR(50) DEFAULT '',
			fields JSON DEFAULT NULL,
			created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
			INDEX idx_timestamp (timestamp),
			INDEX idx_level (level),
			INDEX idx_project (project),
			INDEX idx_logger (logger),
			INDEX idx_created_at (created_at),
			INDEX idx_timestamp_level (timestamp, level),
			INDEX idx_project_timestamp (project, timestamp)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`, m.tableName)

	_, err := m.db.Exec(createTableSQL)
	return err
}

// Write 实现zapcore.WriteSyncer接口
func (m *MySQLCache) Write(p []byte) (n int, err error) {
	// 解析JSON日志
	var logData map[string]interface{}
	if err := json.Unmarshal(p, &logData); err != nil {
		return len(p), err // 忽略解析错误，避免影响日志输出
	}

	// 转换为LogEntry
	entry := m.parseLogEntry(logData)

	// 异步写入
	select {
	case m.writeChan <- entry:
	default:
		// 如果通道满了，直接丢弃
		log.Printf("MySQL写入通道已满，丢弃日志: %s", string(p))
	}

	return len(p), nil
}

// Sync 实现zapcore.WriteSyncer接口
func (m *MySQLCache) Sync() error {
	// 刷新批量缓冲区
	m.flushBatch()
	return nil
}

// parseLogEntry 解析日志条目
func (m *MySQLCache) parseLogEntry(data map[string]interface{}) LogEntry {
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

// isReservedField 检查是否为保留字段
func isReservedField(field string) bool {
	reservedFields := map[string]bool{
		"time":       true,
		"level":      true,
		"msg":        true,
		"logger":     true,
		"caller":     true,
		"project":    true,
		"stacktrace": true,
	}
	return reservedFields[field]
}

// asyncWriter 异步写入协程
func (m *MySQLCache) asyncWriter() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			// 处理剩余的日志
			m.flushRemaining()
			return

		case entry := <-m.writeChan:
			m.batchMutex.Lock()
			m.batchBuffer = append(m.batchBuffer, entry)
			if len(m.batchBuffer) >= 100 {
				m.flushBatchUnsafe()
			}
			m.batchMutex.Unlock()

		case <-ticker.C:
			m.flushBatch()
		}
	}
}

// flushBatch 刷新批量缓冲区
func (m *MySQLCache) flushBatch() {
	m.batchMutex.Lock()
	defer m.batchMutex.Unlock()
	m.flushBatchUnsafe()
}

// flushBatchUnsafe 不安全的批量刷新（需要在锁内调用）
func (m *MySQLCache) flushBatchUnsafe() {
	if len(m.batchBuffer) == 0 {
		return
	}

	if err := m.writeBatchUnsafe(m.batchBuffer); err != nil {
		log.Printf("MySQL批量写入失败: %v", err)
	}

	// 清空缓冲区
	m.batchBuffer = m.batchBuffer[:0]
}

// flushRemaining 刷新剩余的日志
func (m *MySQLCache) flushRemaining() {
	m.batchMutex.Lock()
	defer m.batchMutex.Unlock()

	// 处理通道中剩余的日志
	for {
		select {
		case entry := <-m.writeChan:
			m.batchBuffer = append(m.batchBuffer, entry)
		default:
			goto flush
		}
	}

flush:
	if len(m.batchBuffer) > 0 {
		m.flushBatchUnsafe()
	}
}

// WriteBatch 批量写入日志
func (m *MySQLCache) WriteBatch(ctx context.Context, logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	return m.writeBatchUnsafe(logs)
}

// writeBatchUnsafe 不安全的批量写入
func (m *MySQLCache) writeBatchUnsafe(logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	// 构建批量插入SQL
	placeholders := make([]string, len(logs))
	args := make([]interface{}, 0, len(logs)*8)

	for i, log := range logs {
		placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?)"
		args = append(args,
			log.Timestamp,
			log.Level,
			log.Message,
			log.Logger,
			log.Caller,
			log.Project,
			marshalFields(log.Fields),
			log.CreatedAt,
		)
	}

	sql := fmt.Sprintf(`
		INSERT INTO %s (timestamp, level, message, logger, caller, project, fields, created_at)
		VALUES %s
	`, m.tableName, strings.Join(placeholders, ","))

	_, err := m.db.Exec(sql, args...)
	return err
}

// marshalFields 序列化字段
func marshalFields(fields map[string]interface{}) interface{} {
	if len(fields) == 0 {
		return nil
	}

	data, err := json.Marshal(fields)
	if err != nil {
		return nil
	}

	return string(data)
}

// Query 查询日志
func (m *MySQLCache) Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error) {
	if opts == nil {
		opts = &QueryOptions{}
	}

	// 构建查询SQL
	whereClause, args := m.buildWhereClause(opts)
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", m.tableName, whereClause)

	// 查询总数
	var total int64
	if err := m.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("查询总数失败: %w", err)
	}

	// 构建查询SQL
	orderBy := "ORDER BY timestamp DESC"
	if opts.OrderBy != "" {
		direction := "DESC"
		if opts.OrderDir == "asc" {
			direction = "ASC"
		}
		orderBy = fmt.Sprintf("ORDER BY %s %s", opts.OrderBy, direction)
	}

	limit := "LIMIT 100"
	if opts.Limit > 0 {
		limit = fmt.Sprintf("LIMIT %d", opts.Limit)
		if opts.Offset > 0 {
			limit = fmt.Sprintf("LIMIT %d, %d", opts.Offset, opts.Limit)
		}
	}

	querySQL := fmt.Sprintf(`
		SELECT id, timestamp, level, message, logger, caller, project, fields, created_at
		FROM %s %s %s %s
	`, m.tableName, whereClause, orderBy, limit)

	rows, err := m.db.Query(querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("查询日志失败: %w", err)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		var fieldsJSON sql.NullString

		err := rows.Scan(
			&entry.ID,
			&entry.Timestamp,
			&entry.Level,
			&entry.Message,
			&entry.Logger,
			&entry.Caller,
			&entry.Project,
			&fieldsJSON,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描日志行失败: %w", err)
		}

		// 解析字段JSON
		if fieldsJSON.Valid {
			json.Unmarshal([]byte(fieldsJSON.String), &entry.Fields)
		}

		logs = append(logs, entry)
	}

	return &QueryResult{
		Logs:  logs,
		Total: total,
		Page:  opts.Offset/opts.Limit + 1,
		Size:  len(logs),
	}, nil
}

// buildWhereClause 构建WHERE子句
func (m *MySQLCache) buildWhereClause(opts *QueryOptions) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if opts.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *opts.StartTime)
	}

	if opts.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, *opts.EndTime)
	}

	if len(opts.Levels) > 0 {
		placeholders := make([]string, len(opts.Levels))
		for i := range opts.Levels {
			placeholders[i] = "?"
		}
		conditions = append(conditions, fmt.Sprintf("level IN (%s)", strings.Join(placeholders, ",")))
		for _, level := range opts.Levels {
			args = append(args, level)
		}
	}

	if len(opts.Projects) > 0 {
		placeholders := make([]string, len(opts.Projects))
		for i := range opts.Projects {
			placeholders[i] = "?"
		}
		conditions = append(conditions, fmt.Sprintf("project IN (%s)", strings.Join(placeholders, ",")))
		for _, project := range opts.Projects {
			args = append(args, project)
		}
	}

	if len(opts.Loggers) > 0 {
		placeholders := make([]string, len(opts.Loggers))
		for i := range opts.Loggers {
			placeholders[i] = "?"
		}
		conditions = append(conditions, fmt.Sprintf("logger IN (%s)", strings.Join(placeholders, ",")))
		for _, logger := range opts.Loggers {
			args = append(args, logger)
		}
	}

	if len(opts.Keywords) > 0 {
		var keywordConditions []string
		for _, keyword := range opts.Keywords {
			keywordConditions = append(keywordConditions, "message LIKE ?")
			args = append(args, "%"+keyword+"%")
		}
		conditions = append(conditions, "("+strings.Join(keywordConditions, " OR ")+")")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	return whereClause, args
}

// GetByID 根据ID查询日志
func (m *MySQLCache) GetByID(ctx context.Context, id int64) (*LogEntry, error) {
	querySQL := fmt.Sprintf(`
		SELECT id, timestamp, level, message, logger, caller, project, fields, created_at
		FROM %s WHERE id = ?
	`, m.tableName)

	row := m.db.QueryRow(querySQL, id)

	var entry LogEntry
	var fieldsJSON sql.NullString

	err := row.Scan(
		&entry.ID,
		&entry.Timestamp,
		&entry.Level,
		&entry.Message,
		&entry.Logger,
		&entry.Caller,
		&entry.Project,
		&fieldsJSON,
		&entry.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询日志失败: %w", err)
	}

	// 解析字段JSON
	if fieldsJSON.Valid {
		json.Unmarshal([]byte(fieldsJSON.String), &entry.Fields)
	}

	return &entry, nil
}

// Count 统计日志数量
func (m *MySQLCache) Count(ctx context.Context, opts *QueryOptions) (int64, error) {
	if opts == nil {
		opts = &QueryOptions{}
	}

	whereClause, args := m.buildWhereClause(opts)
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", m.tableName, whereClause)

	var count int64
	err := m.db.QueryRow(countSQL, args...).Scan(&count)
	return count, err
}

// DeleteExpired 删除过期日志
func (m *MySQLCache) DeleteExpired(ctx context.Context, before time.Time) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE created_at < ?", m.tableName)
	result, err := m.db.Exec(deleteSQL, before)
	if err != nil {
		return fmt.Errorf("删除过期日志失败: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("删除了 %d 条过期日志", rowsAffected)

	return nil
}

// Ping 健康检查
func (m *MySQLCache) Ping(ctx context.Context) error {
	return m.db.Ping()
}

// Close 关闭连接
func (m *MySQLCache) Close() error {
	// 停止异步写入
	m.cancel()
	m.wg.Wait()

	// 关闭通道
	close(m.writeChan)

	// 关闭数据库连接
	return m.db.Close()
}

// WriteLogs 写入日志条目（实现LogCache接口）
func (m *MySQLCache) WriteLogs(ctx context.Context, logs ...LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	return m.WriteBatch(ctx, logs)
}

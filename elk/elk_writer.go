package elk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"go.uber.org/zap/zapcore"
)

// ELKConfig ELK配置结构体
type ELKConfig struct {
	// Elasticsearch地址
	Addresses []string `json:"addresses" mapstructure:"addresses"`
	// 用户名
	Username string `json:",optional" mapstructure:"username"`
	// 密码
	Password string `json:",optional" mapstructure:"password"`
	// API密钥
	APIKey string `json:",optional" mapstructure:"apiKey"`
	// 服务令牌
	ServiceToken string `json:",optional" mapstructure:"serviceToken"`
	// 索引名称前缀，默认使用项目名
	IndexPrefix string `json:",optional" mapstructure:"indexPrefix"`
	// 日志刷新间隔，单位秒
	FlushSec int64 `json:",default=5" mapstructure:"flushSec"`
	// 最大缓存日志数量
	MaxCount int64 `json:",default=100" mapstructure:"maxCount"`
	// 批量大小
	BulkSize int `json:",default=100" mapstructure:"bulkSize"`
	// 是否异步发送
	Async bool `json:",default=true" mapstructure:"async"`
}

// ELKWriter ELK日志写入器
type ELKWriter struct {
	client      *elasticsearch.Client
	config      *ELKConfig
	buffer      []map[string]interface{}
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	flushTicker *time.Ticker
}

// NewELKWriter 创建ELK写入器
func NewELKWriter(config *ELKConfig) (*ELKWriter, error) {
	if len(config.Addresses) == 0 {
		config.Addresses = []string{"http://localhost:9200"}
	}
	if config.FlushSec == 0 {
		config.FlushSec = 5
	}
	if config.MaxCount == 0 {
		config.MaxCount = 100
	}
	if config.BulkSize == 0 {
		config.BulkSize = 100
	}

	// 配置Elasticsearch客户端
	esConfig := elasticsearch.Config{
		Addresses: config.Addresses,
	}

	// 设置认证
	if config.Username != "" && config.Password != "" {
		esConfig.Username = config.Username
		esConfig.Password = config.Password
	} else if config.APIKey != "" {
		esConfig.APIKey = config.APIKey
	} else if config.ServiceToken != "" {
		esConfig.ServiceToken = config.ServiceToken
	}

	client, err := elasticsearch.NewClient(esConfig)
	if err != nil {
		return nil, fmt.Errorf("创建Elasticsearch客户端失败: %w", err)
	}

	// 测试连接
	res, err := client.Info()
	if err != nil {
		return nil, fmt.Errorf("连接Elasticsearch失败: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("Elasticsearch响应错误: %s", res.String())
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &ELKWriter{
		client:      client,
		config:      config,
		buffer:      make([]map[string]interface{}, 0, config.MaxCount),
		ctx:         ctx,
		cancel:      cancel,
		flushTicker: time.NewTicker(time.Duration(config.FlushSec) * time.Second),
	}

	// 启动异步刷新
	if config.Async {
		go w.startFlushRoutine()
	}

	return w, nil
}

// Write 实现zapcore.WriteSyncer接口
func (w *ELKWriter) Write(p []byte) (n int, err error) {
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		// 如果JSON解析失败，创建一个简单的日志条目
		logEntry = map[string]interface{}{
			"@timestamp": time.Now().Format(time.RFC3339),
			"message":    string(p),
			"level":      "unknown",
		}
	}

	// 确保时间戳字段存在
	if _, exists := logEntry["@timestamp"]; !exists {
		logEntry["@timestamp"] = time.Now().Format(time.RFC3339)
	}

	// 添加项目名称
	if w.config.IndexPrefix != "" {
		logEntry["project"] = w.config.IndexPrefix
	}

	w.mu.Lock()
	w.buffer = append(w.buffer, logEntry)
	bufferLen := len(w.buffer)
	w.mu.Unlock()

	// 如果达到批量大小或最大数量，立即发送
	if bufferLen >= w.config.BulkSize || bufferLen >= int(w.config.MaxCount) {
		w.flush()
	}

	return len(p), nil
}

// Sync 实现zapcore.WriteSyncer接口
func (w *ELKWriter) Sync() error {
	w.flush()
	return nil
}

// Close 关闭写入器
func (w *ELKWriter) Close() error {
	w.cancel()
	w.flushTicker.Stop()
	return w.Sync()
}

// startFlushRoutine 启动定时刷新协程
func (w *ELKWriter) startFlushRoutine() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.flushTicker.C:
			w.flush()
		}
	}
}

// flush 刷新缓冲区到Elasticsearch
func (w *ELKWriter) flush() {
	w.mu.Lock()
	if len(w.buffer) == 0 {
		w.mu.Unlock()
		return
	}

	// 复制缓冲区并清空
	bufferCopy := make([]map[string]interface{}, len(w.buffer))
	copy(bufferCopy, w.buffer)
	w.buffer = w.buffer[:0]
	w.mu.Unlock()

	if len(bufferCopy) == 0 {
		return
	}

	// 构建批量请求
	var buf bytes.Buffer
	for _, entry := range bufferCopy {
		// 索引元数据
		indexName := w.getIndexName(entry)
		meta := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
			},
		}

		metaBytes, _ := json.Marshal(meta)
		buf.Write(metaBytes)
		buf.WriteByte('\n')

		// 文档内容
		docBytes, _ := json.Marshal(entry)
		buf.Write(docBytes)
		buf.WriteByte('\n')
	}

	// 发送批量请求
	req := esapi.BulkRequest{
		Body:    &buf,
		Refresh: "false",
	}

	res, err := req.Do(context.Background(), w.client)
	if err != nil {
		log.Printf("ELK写入失败: %v", err)
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		log.Printf("ELK批量请求失败: %s", res.String())
		return
	}
}

// getIndexName 获取索引名称
func (w *ELKWriter) getIndexName(entry map[string]interface{}) string {
	prefix := w.config.IndexPrefix
	if prefix == "" {
		prefix = "zlog"
	}

	// 使用日期作为索引后缀
	date := time.Now().Format("2006.01.02")
	return fmt.Sprintf("%s-%s", prefix, date)
}

// ELKWriteSyncer 创建ELK写入同步器
func ELKWriteSyncer(config *ELKConfig) (zapcore.WriteSyncer, error) {
	writer, err := NewELKWriter(config)
	if err != nil {
		return nil, err
	}
	return writer, nil
}

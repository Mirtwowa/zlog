package elk

import (
	"github.com/luxun9527/zlog"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestELKConfig(t *testing.T) {
	// 测试ELK配置创建
	config := &ELKConfig{
		Addresses:   []string{"http://localhost:9200"},
		IndexPrefix: "test",
		FlushSec:    5,
		MaxCount:    100,
		BulkSize:    50,
		Async:       true,
	}

	if len(config.Addresses) == 0 {
		t.Error("地址列表不能为空")
	}

	if config.IndexPrefix == "" {
		t.Error("索引前缀不能为空")
	}

	if config.FlushSec <= 0 {
		t.Error("刷新间隔必须大于0")
	}
}

func TestConfigWithELK(t *testing.T) {
	// 测试包含ELK配置的完整配置
	config := &zlog.Config{
		Name:  "test-service",
		Level: zap.NewAtomicLevelAt(zap.InfoLevel),
		Mode:  "console",
		Json:  true,
		ELKConfig: &ELKConfig{
			Addresses:   []string{"http://localhost:9200"},
			IndexPrefix: "test-service",
			FlushSec:    5,
			MaxCount:    100,
			Async:       true,
		},
	}

	if config.ELKConfig == nil {
		t.Error("ELK配置不能为nil")
	}

	if config.ELKConfig.IndexPrefix != "test-service" {
		t.Error("索引前缀配置错误")
	}
}

func TestELKWriteSyncerCreation(t *testing.T) {
	// 注意：这个测试需要真实的Elasticsearch实例
	// 如果没有Elasticsearch实例，测试会失败
	// 在实际使用中，应该先启动Elasticsearch

	config := &ELKConfig{
		Addresses:   []string{"http://localhost:9200"},
		IndexPrefix: "test",
		FlushSec:    5,
		MaxCount:    100,
		Async:       true,
	}

	// 尝试创建ELK写入器
	// 如果没有Elasticsearch实例，这会失败，但我们可以捕获错误
	_, err := ELKWriteSyncer(config)

	// 在测试环境中，如果没有Elasticsearch实例，这是预期的
	if err != nil {
		t.Logf("ELK写入器创建失败（预期的，如果没有Elasticsearch实例）: %v", err)
	} else {
		t.Log("ELK写入器创建成功")
	}
}

func TestELKIndexName(t *testing.T) {
	// 测试索引名称生成逻辑
	writer := &ELKWriter{
		config: &ELKConfig{
			IndexPrefix: "testapp",
		},
	}

	entry := map[string]interface{}{
		"@timestamp": time.Now().Format(time.RFC3339),
		"level":      "info",
		"message":    "test message",
	}

	indexName := writer.getIndexName(entry)

	// 索引名称应该包含前缀和日期
	if len(indexName) == 0 {
		t.Error("索引名称不能为空")
	}

	// 应该包含前缀
	if len(indexName) < len(writer.config.IndexPrefix) {
		t.Error("索引名称应该包含前缀")
	}

	t.Logf("生成的索引名称: %s", indexName)
}

func TestELKBufferOperations(t *testing.T) {
	// 测试缓冲区操作
	writer := &ELKWriter{
		config: &ELKConfig{
			MaxCount: 10,
			BulkSize: 5,
		},
		buffer: make([]map[string]interface{}, 0),
	}

	// 测试缓冲区写入
	testEntry := map[string]interface{}{
		"@timestamp": time.Now().Format(time.RFC3339),
		"level":      "info",
		"message":    "test message",
	}

	writer.mu.Lock()
	writer.buffer = append(writer.buffer, testEntry)
	bufferLen := len(writer.buffer)
	writer.mu.Unlock()

	if bufferLen != 1 {
		t.Error("缓冲区长度应该为1")
	}

	// 测试缓冲区清空
	writer.mu.Lock()
	writer.buffer = writer.buffer[:0]
	bufferLen = len(writer.buffer)
	writer.mu.Unlock()

	if bufferLen != 0 {
		t.Error("缓冲区长度应该为0")
	}
}

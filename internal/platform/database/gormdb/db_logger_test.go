package gormdb

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type gormLogRecorder struct {
	bytes.Buffer
}

func (recorder *gormLogRecorder) Printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(&recorder.Buffer, format, args...)
}

func TestReleaseGORMLoggerDoesNotExposeQueryParameters(t *testing.T) {
	recorder := &gormLogRecorder{}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: newGORMLogger(" release ", recorder),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	secret := "must-not-appear-in-production-database-logs"
	if err := db.Exec("INSERT INTO missing_table (secret) VALUES (?)", secret).Error; err == nil {
		t.Fatal("expected missing-table error")
	}

	output := recorder.String()
	if output == "" {
		t.Fatal("expected database error to be logged")
	}
	if strings.Contains(output, secret) {
		t.Fatalf("release database log exposed a query parameter: %s", output)
	}
	if !strings.Contains(output, "missing_table") {
		t.Fatalf("release database log should retain useful query context: %s", output)
	}
}

// TestDebugGORMLoggerDoesNotExposeQueryParameters 开发模式（非 release）同样不得把参数值写进日志。
// 背景：2026-09-25 发现 debug 模式使用 gormlogger.Default 会打印完整 SQL，
// 导致 site_connections 的 api_key 明文出现在日志里（见 Task 8 日志脱敏检查）。
func TestDebugGORMLoggerDoesNotExposeQueryParameters(t *testing.T) {
	recorder := &gormLogRecorder{}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: newGORMLogger("debug", recorder),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	secret := "must-not-appear-in-development-database-logs"
	if err := db.Exec("INSERT INTO missing_table (secret) VALUES (?)", secret).Error; err == nil {
		t.Fatal("expected missing-table error")
	}

	output := recorder.String()
	if output == "" {
		t.Fatal("expected database error to be logged")
	}
	if strings.Contains(output, secret) {
		t.Fatalf("debug database log exposed a query parameter: %s", output)
	}
	if !strings.Contains(output, "missing_table") {
		t.Fatalf("debug database log should retain useful query context: %s", output)
	}
}

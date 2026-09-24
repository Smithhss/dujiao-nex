package settingsintegration

import (
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

const (
	upstreamSyncIntervalMinDefault = 5
	upstreamSyncIntervalMinMin     = 5
	upstreamSyncIntervalMinMax     = 1440

	upstreamSyncPageSizeDefault    = 50
	upstreamSyncPageSizeMin        = 10
	upstreamSyncPageSizeMax        = 200
	upstreamSyncMaxPagesDefault    = 200
	upstreamSyncMaxPagesMin        = 10
	upstreamSyncMaxPagesMax        = 500
	upstreamSyncConnConcurrencyDef = 3
	upstreamSyncConnConcurrencyMin = 1
	upstreamSyncConnConcurrencyMax = 10
)

// 上游商品映射后的商品级价格策略。
const (
	// UpstreamPriceStrategyUpstream 使用上游商品级价格（默认，保持既有行为，便于人工调价）
	UpstreamPriceStrategyUpstream = "upstream_price"
	// UpstreamPriceStrategyMinSKU 使用该商品所有 SKU 中的最低本地价（避免卡片显示高价 SKU）
	UpstreamPriceStrategyMinSKU = "min_sku"
)

// UpstreamSyncConfig 是上游同步设置的 typed representation。
type UpstreamSyncConfig struct {
	IntervalMinutes           int  `json:"interval_minutes"`
	PreOrderStockCheckEnabled bool `json:"pre_order_stock_check_enabled"`
	SyncPageSize              int  `json:"sync_page_size"`
	SyncMaxPages              int  `json:"sync_max_pages"`
	SyncConnConcurrency       int  `json:"sync_conn_concurrency"`
	// PriceStrategy 决定映射商品的商品级价格如何产生：upstream_price（上游价，默认）或 min_sku（最低 SKU 价）
	PriceStrategy string `json:"price_strategy"`
}

// DefaultUpstreamSyncConfig 返回稳定的上游同步默认设置。
func DefaultUpstreamSyncConfig() UpstreamSyncConfig {
	return UpstreamSyncConfig{
		IntervalMinutes:           upstreamSyncIntervalMinDefault,
		PreOrderStockCheckEnabled: true,
		SyncPageSize:              upstreamSyncPageSizeDefault,
		SyncMaxPages:              upstreamSyncMaxPagesDefault,
		SyncConnConcurrency:       upstreamSyncConnConcurrencyDef,
		PriceStrategy:             UpstreamPriceStrategyUpstream,
	}
}

// NormalizeUpstreamSyncConfig 归一化上游同步范围。
func NormalizeUpstreamSyncConfig(config UpstreamSyncConfig) UpstreamSyncConfig {
	if config.IntervalMinutes < upstreamSyncIntervalMinMin {
		config.IntervalMinutes = upstreamSyncIntervalMinDefault
	}
	if config.IntervalMinutes > upstreamSyncIntervalMinMax {
		config.IntervalMinutes = upstreamSyncIntervalMinMax
	}
	if config.SyncPageSize < upstreamSyncPageSizeMin {
		config.SyncPageSize = upstreamSyncPageSizeDefault
	}
	if config.SyncPageSize > upstreamSyncPageSizeMax {
		config.SyncPageSize = upstreamSyncPageSizeMax
	}
	if config.SyncMaxPages < upstreamSyncMaxPagesMin {
		config.SyncMaxPages = upstreamSyncMaxPagesDefault
	}
	if config.SyncMaxPages > upstreamSyncMaxPagesMax {
		config.SyncMaxPages = upstreamSyncMaxPagesMax
	}
	if config.SyncConnConcurrency < upstreamSyncConnConcurrencyMin {
		config.SyncConnConcurrency = upstreamSyncConnConcurrencyDef
	}
	if config.SyncConnConcurrency > upstreamSyncConnConcurrencyMax {
		config.SyncConnConcurrency = upstreamSyncConnConcurrencyMax
	}
	config.PriceStrategy = NormalizeUpstreamPriceStrategy(config.PriceStrategy)
	return config
}

// NormalizeUpstreamPriceStrategy 归一化价格策略，未知值回落到上游商品价。
func NormalizeUpstreamPriceStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case UpstreamPriceStrategyMinSKU:
		return UpstreamPriceStrategyMinSKU
	default:
		return UpstreamPriceStrategyUpstream
	}
}

// DecodeUpstreamSyncConfig 从持久化 JSON 解码，并对缺失字段使用 fallback。
func DecodeUpstreamSyncConfig(raw jsonmap.JSON, fallback UpstreamSyncConfig) UpstreamSyncConfig {
	result := NormalizeUpstreamSyncConfig(fallback)
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldUpstreamSyncIntervalMin]); err == nil {
		result.IntervalMinutes = parsed
	}
	if value, exists := raw[constants.SettingFieldUpstreamPreOrderCheck]; exists {
		result.PreOrderStockCheckEnabled = settingsvalue.ParseBool(value)
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldUpstreamSyncPageSize]); err == nil {
		result.SyncPageSize = parsed
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldUpstreamSyncMaxPages]); err == nil {
		result.SyncMaxPages = parsed
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldUpstreamSyncConcurrency]); err == nil {
		result.SyncConnConcurrency = parsed
	}
	if value, exists := raw[constants.SettingFieldUpstreamPriceStrategy]; exists {
		result.PriceStrategy = NormalizeUpstreamPriceStrategy(settingsvalue.NormalizeTextWithRuneLimit(value, 32))
	}
	return NormalizeUpstreamSyncConfig(result)
}

// EncodeUpstreamSyncConfig 把 typed setting 编码为稳定的持久化 JSON。
func EncodeUpstreamSyncConfig(config UpstreamSyncConfig) jsonmap.JSON {
	normalized := NormalizeUpstreamSyncConfig(config)
	return jsonmap.JSON{
		constants.SettingFieldUpstreamSyncIntervalMin: normalized.IntervalMinutes,
		constants.SettingFieldUpstreamPreOrderCheck:   normalized.PreOrderStockCheckEnabled,
		constants.SettingFieldUpstreamSyncPageSize:    normalized.SyncPageSize,
		constants.SettingFieldUpstreamSyncMaxPages:    normalized.SyncMaxPages,
		constants.SettingFieldUpstreamSyncConcurrency: normalized.SyncConnConcurrency,
		constants.SettingFieldUpstreamPriceStrategy:   normalized.PriceStrategy,
	}
}

// NormalizeUpstreamSyncConfigJSON 是 Registry 使用的原始 JSON 写入策略。
func NormalizeUpstreamSyncConfigJSON(raw jsonmap.JSON) jsonmap.JSON {
	return EncodeUpstreamSyncConfig(DecodeUpstreamSyncConfig(raw, DefaultUpstreamSyncConfig()))
}

// UpstreamSyncFallback 将 config.yml 的 duration fallback 转为 typed setting。
func UpstreamSyncFallback(fallbackInterval string) UpstreamSyncConfig {
	fallback := DefaultUpstreamSyncConfig()
	if minutes := parseDurationToMinutes(fallbackInterval); minutes > 0 {
		fallback.IntervalMinutes = minutes
	}
	return NormalizeUpstreamSyncConfig(fallback)
}

// FormatUpstreamSyncIntervalForScheduler 返回 asynq scheduler 支持的 @every 间隔。
func FormatUpstreamSyncIntervalForScheduler(duration time.Duration) string {
	if duration < time.Duration(upstreamSyncIntervalMinMin)*time.Minute {
		duration = time.Duration(upstreamSyncIntervalMinDefault) * time.Minute
	}
	return fmt.Sprintf("%dm", int(duration/time.Minute))
}

func parseDurationToMinutes(value string) int {
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	minutes := int(duration / time.Minute)
	if minutes <= 0 {
		return 0
	}
	return minutes
}

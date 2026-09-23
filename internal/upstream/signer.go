package upstream

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// HeaderApiKey API Key header
	HeaderApiKey = "Dujiao-Next-Api-Key"
	// HeaderTimestamp 时间戳 header
	HeaderTimestamp = "Dujiao-Next-Timestamp"
	// HeaderSignature 签名 header
	HeaderSignature = "Dujiao-Next-Signature"
	// HeaderNonce v2 签名要求的随机串 header（每次请求唯一，推荐 UUID v4）
	HeaderNonce = "Dujiao-Next-Nonce"

	// MaxTimestampSkew 最大时间戳偏差（秒）
	MaxTimestampSkew = 60
)

// Sign 生成 HMAC-SHA256 签名
// signString = "{method}\n{path}\n{timestamp}\n{body_md5}"
func Sign(secret, method, path string, timestamp int64, body []byte) string {
	bodyMD5 := md5Hex(body)
	signString := fmt.Sprintf("%s\n%s\n%d\n%s", method, path, timestamp, bodyMD5)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signString))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 验证签名
func Verify(secret, method, path, signature string, timestamp int64, body []byte) bool {
	expected := Sign(secret, method, path, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// IsTimestampValid 检查时间戳是否在有效范围内
func IsTimestampValid(timestamp int64) bool {
	now := time.Now().Unix()
	return math.Abs(float64(now-timestamp)) <= MaxTimestampSkew
}

// ParseTimestamp 解析时间戳字符串
func ParseTimestamp(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func md5Hex(data []byte) string {
	h := md5.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// SignV2 生成上游《余额自动下单 API 接口文档 v2.0》要求的 HMAC-SHA256 签名。
//
// canonical = METHOD + "\n" + PATH + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + SHA256_HEX(RAW_BODY)
//   - METHOD 大写；PATH 只含路径（不含域名与 query）；
//   - TIMESTAMP 为 Unix 秒；NONCE 为每次请求唯一的随机串（UUID v4）；
//   - SHA256 基于实际发送的原始 body 字节，空 body 使用 SHA256 空串摘要；
//   - secret 按 UTF-8 原文字节作为 HMAC key，禁止 hex/Base64 解码。
//
// 与 v1 的 Sign 的差异：body 摘要由 MD5 改为 SHA256，canonical 增加 NONCE 段。
func SignV2(secret, method, path string, timestamp int64, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	canonical := strings.Join([]string{
		strings.ToUpper(method),
		path,
		fmt.Sprintf("%d", timestamp),
		nonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

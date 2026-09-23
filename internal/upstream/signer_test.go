package upstream

import (
	"strings"
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	secret := "test-secret-key-12345"
	method := "POST"
	path := "/api/v1/upstream/orders"
	timestamp := time.Now().Unix()
	body := []byte(`{"sku_id":1,"quantity":1}`)

	sig := Sign(secret, method, path, timestamp, body)
	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	// 正确的签名应该验证通过
	if !Verify(secret, method, path, sig, timestamp, body) {
		t.Fatal("signature verification should pass")
	}

	// 错误的 secret 应该验证失败
	if Verify("wrong-secret", method, path, sig, timestamp, body) {
		t.Fatal("signature verification should fail with wrong secret")
	}

	// 错误的 body 应该验证失败
	if Verify(secret, method, path, sig, timestamp, []byte(`{"sku_id":2}`)) {
		t.Fatal("signature verification should fail with different body")
	}

	// 错误的 path 应该验证失败
	if Verify(secret, method, "/api/v1/upstream/ping", sig, timestamp, body) {
		t.Fatal("signature verification should fail with different path")
	}
}

func TestSignEmptyBody(t *testing.T) {
	secret := "test-secret"
	sig1 := Sign(secret, "GET", "/test", 1000, nil)
	sig2 := Sign(secret, "GET", "/test", 1000, []byte{})

	// nil 和空 []byte 的 MD5 相同
	if sig1 != sig2 {
		t.Fatal("nil body and empty body should produce same signature")
	}
}

func TestIsTimestampValid(t *testing.T) {
	now := time.Now().Unix()

	if !IsTimestampValid(now) {
		t.Fatal("current timestamp should be valid")
	}

	if !IsTimestampValid(now - (MaxTimestampSkew / 2)) {
		t.Fatalf("timestamp within skew window should be valid: skew=%d", MaxTimestampSkew)
	}

	if IsTimestampValid(now - (MaxTimestampSkew + 1)) {
		t.Fatalf("timestamp older than skew window should be invalid: skew=%d", MaxTimestampSkew)
	}

	if IsTimestampValid(now + (MaxTimestampSkew + 1)) {
		t.Fatalf("future timestamp beyond skew window should be invalid: skew=%d", MaxTimestampSkew)
	}
}

func TestParseTimestamp(t *testing.T) {
	ts, err := ParseTimestamp("1709625600")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts != 1709625600 {
		t.Fatalf("expected 1709625600, got %d", ts)
	}

	_, err = ParseTimestamp("not-a-number")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

// TestSignV2FixedVector 校验上游《余额自动下单 API 接口文档 v2.0》§3.3 的固定测试向量。
//
// 注意：该文档正文粘贴的 secret 只有 112 位（"0123456789abcdef" × 7），
// 但同段声明为 128 位；用 128 位（× 8）才能复现文档公布的签名（见 docs/reference 与计划附录 A）。
func TestSignV2FixedVector(t *testing.T) {
	secret := strings.Repeat("0123456789abcdef", 8) // 128 位
	const nonce = "550e8400-e29b-41d4-a716-446655440000"

	got := SignV2(secret, "POST", "/api/v1/upstream/ping", 1709625600, nonce, nil)

	const want = "09cf5cedb04ba4b32552c7e9d8c533368eeb48dd9ba1b72fd1e2c151e9487819"
	if got != want {
		t.Fatalf("v2 signature mismatch\n got=%s\nwant=%s", got, want)
	}
}

// TestSignV2UsesRawBodyBytes 签名必须基于"实际发送的原始字节"：JSON 空格变化必须导致签名变化。
func TestSignV2UsesRawBodyBytes(t *testing.T) {
	const secret = "test-secret"
	a := SignV2(secret, "POST", "/api/v1/upstream/orders", 1709625600, "nonce-a", []byte(`{"sku_id":1,"quantity":1}`))
	b := SignV2(secret, "POST", "/api/v1/upstream/orders", 1709625600, "nonce-a", []byte(`{"sku_id":1,"quantity": 1}`))
	if a == b {
		t.Fatal("body 变了签名必须变：签名基于原始 body 字节")
	}
}

// TestSignV2NonceChangesSignature 每次请求换 nonce 必须改变签名（重放保护）。
func TestSignV2NonceChangesSignature(t *testing.T) {
	const secret = "test-secret"
	a := SignV2(secret, "POST", "/api/v1/upstream/ping", 1709625600, "nonce-a", nil)
	b := SignV2(secret, "POST", "/api/v1/upstream/ping", 1709625600, "nonce-b", nil)
	if a == b {
		t.Fatal("nonce 变了签名必须变")
	}
}

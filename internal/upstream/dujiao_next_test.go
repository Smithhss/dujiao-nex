package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

// newTestAdapter 构造一个指向 httptest 服务器的适配器。
func newTestAdapter(t *testing.T, handler http.HandlerFunc) (*DujiaoNextAdapter, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	conn := &siteconnectiondomain.Connection{
		BaseURL:   server.URL,
		ApiKey:    "test-key",
		ApiSecret: "test-secret",
	}
	return NewDujiaoNextAdapter(conn, t.TempDir()), server.Close
}

// TestListProductsClampsPageSizeToDocMax 上游文档 §6.1 规定 page_size 最大 50，
// 后台设置上限更高时适配器必须自行钳制，避免上游 400。
func TestListProductsClampsPageSizeToDocMax(t *testing.T) {
	var gotQuery string
	adapter, closeFn := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[],"total":0,"page":1,"page_size":50}`))
	})
	defer closeFn()

	if _, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1, PageSize: 200}); err != nil {
		t.Fatalf("ListProducts(PageSize=200): %v", err)
	}
	if !strings.Contains(gotQuery, "page_size=50") {
		t.Fatalf("page_size 应被钳制为 50，实际 query=%s", gotQuery)
	}

	if _, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1}); err != nil {
		t.Fatalf("ListProducts(PageSize=0): %v", err)
	}
	if !strings.Contains(gotQuery, "page_size=50") {
		t.Fatalf("未指定 page_size 时应使用文档默认值 50，实际 query=%s", gotQuery)
	}
}

// TestCreateOrderSendsWalletPaymentModeAndOmitsCallbackURL 上游文档 §7：
// payment_mode 推荐显式传 wallet；callback_url 当前不支持必须省略。
func TestCreateOrderSendsWalletPaymentModeAndOmitsCallbackURL(t *testing.T) {
	var body map[string]any
	adapter, closeFn := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"order_id":1,"status":"completed"}`))
	})
	defer closeFn()

	if _, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
		SKUID:             1,
		Quantity:          1,
		DownstreamOrderNo: "DJ20261007000000000001",
	}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	if body["payment_mode"] != "wallet" {
		t.Fatalf("payment_mode 应为 wallet，实际 %v", body["payment_mode"])
	}
	if _, exists := body["callback_url"]; exists {
		t.Fatalf("不应发送 callback_url，实际 body=%v", body)
	}
}

// TestCreateOrderRejectsQuantityOutOfDocRange 上游文档 §7：quantity 为正整数且单次上限 100。
// 超出范围时必须本地拦截，不发出上游请求（避免无意义的重试与上游 400）。
func TestCreateOrderRejectsQuantityOutOfDocRange(t *testing.T) {
	called := false
	adapter, closeFn := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"order_id":1,"status":"completed"}`))
	})
	defer closeFn()

	for _, qty := range []int{0, -1, 101, 1000} {
		if _, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
			SKUID: 1, Quantity: qty, DownstreamOrderNo: "DJ20261007000000000001",
		}); err == nil {
			t.Fatalf("quantity=%d 应被本地拒绝", qty)
		}
	}
	if called {
		t.Fatal("数量非法时不应调用上游")
	}

	if _, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
		SKUID: 1, Quantity: 100, DownstreamOrderNo: "DJ20261007000000000001",
	}); err != nil {
		t.Fatalf("quantity=100 属合法边界，应放行: %v", err)
	}
	if !called {
		t.Fatal("合法数量应调用上游")
	}
}

// TestUpstreamNonOKResponseDoesNotLeakBody 上游文档 §8：卡密等敏感内容不得进入日志/错误信息。
// 非 200 且没有 error_code 时（例如网关返回 HTML 错误页），错误信息不得携带原始响应体。
func TestUpstreamNonOKResponseDoesNotLeakBody(t *testing.T) {
	const secret = "CARD-SECRET-SHOULD-NOT-LEAK-1234"
	adapter, closeFn := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway: " + secret + "</html>"))
	})
	defer closeFn()

	_, err := adapter.GetOrder(context.Background(), 1)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("错误信息不应包含原始响应体: %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("错误信息应保留 HTTP 状态码: %v", err)
	}
}

// TestAdapterSharesPackageLevelLimiter 适配器每次新建，但必须共享包级限速器；
// 配额耗尽时 doRequest 应返回 ErrUpstreamThrottled 且不发出请求。
func TestAdapterSharesPackageLevelLimiter(t *testing.T) {
	var requests int32
	adapter, closeFn := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[],"total":0,"page":1,"page_size":50}`))
	})
	defer closeFn()

	if adapter.limiter == nil {
		t.Fatal("适配器应绑定包级限速器")
	}

	// 用该凭证的限速器把 60 次/分钟配额耗尽（计数器共享，等价于 60 次真实请求）。
	for i := 0; i < maxUpstreamRequestsPerMinute; i++ {
		release, _, err := adapter.limiter.acquire(context.Background(), classRead)
		if err != nil {
			t.Fatalf("预热第 %d 次失败: %v", i+1, err)
		}
		release()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adapter.ListProducts(ctx, ListProductsOpts{Page: 1, PageSize: 50})
	if !errors.Is(err, ErrUpstreamThrottled) {
		t.Fatalf("配额耗尽后应返回 ErrUpstreamThrottled，实际 %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("被限速时不应发出请求，实际发出 %d 次", got)
	}
}

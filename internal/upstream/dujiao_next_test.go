package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

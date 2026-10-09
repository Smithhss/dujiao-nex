package upstream

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock 让窗口类测试可以瞬时推进时间。
type fakeClock struct {
	mu    sync.Mutex
	t     time.Time
	slept time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.slept += d
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) totalSlept() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.slept
}

// TestConnectionLimiterBlocksSixtyFirstRequestPerMinute 上游文档 §11：单凭证 ≤60 次/分钟。
func TestConnectionLimiterBlocksSixtyFirstRequestPerMinute(t *testing.T) {
	fake := newFakeClock()
	limiter := newConnectionLimiter("test-global", fake.now, fake.sleep)

	for i := 0; i < maxUpstreamRequestsPerMinute; i++ {
		release, waited, err := limiter.acquire(context.Background(), classRead)
		if err != nil {
			t.Fatalf("第 %d 次请求异常: %v", i+1, err)
		}
		if waited != 0 {
			t.Fatalf("前 %d 次不应等待，第 %d 次等待 %v", maxUpstreamRequestsPerMinute, i+1, waited)
		}
		release()
	}

	release, _, err := limiter.acquire(context.Background(), classRead)
	if err != nil {
		t.Fatalf("第 %d 次请求异常: %v", maxUpstreamRequestsPerMinute+1, err)
	}
	release()

	if slept := fake.totalSlept(); slept < 59*time.Second {
		t.Fatalf("第 %d 次请求应等待接近 60 秒，实际 %v", maxUpstreamRequestsPerMinute+1, slept)
	}
}

// TestConnectionLimiterBlocksSixthOrderWithinTenSeconds 上游文档 §11：下单 ≤5 次/10 秒。
func TestConnectionLimiterBlocksSixthOrderWithinTenSeconds(t *testing.T) {
	fake := newFakeClock()
	limiter := newConnectionLimiter("test-orders", fake.now, fake.sleep)

	for i := 0; i < maxUpstreamOrdersPer10s; i++ {
		release, _, err := limiter.acquire(context.Background(), classWrite)
		if err != nil {
			t.Fatalf("第 %d 次下单异常: %v", i+1, err)
		}
		release()
	}
	if slept := fake.totalSlept(); slept != 0 {
		t.Fatalf("前 %d 次下单不应等待，实际等待 %v", maxUpstreamOrdersPer10s, slept)
	}

	release, _, err := limiter.acquire(context.Background(), classWrite)
	if err != nil {
		t.Fatalf("第 %d 次下单异常: %v", maxUpstreamOrdersPer10s+1, err)
	}
	release()

	if slept := fake.totalSlept(); slept < 9*time.Second {
		t.Fatalf("第 %d 次下单应等待接近 10 秒，实际 %v", maxUpstreamOrdersPer10s+1, slept)
	}
}

// TestConnectionLimiterEnforcesConcurrency 写并发 ≤2、读并发 ≤8（窗口用 fake sleep 跳过节奏限制）。
func TestConnectionLimiterEnforcesConcurrency(t *testing.T) {
	fake := newFakeClock()
	limiter := newConnectionLimiter("test-concurrency", fake.now, fake.sleep)

	run := func(class requestClass, goroutines int) int64 {
		var current, peak int64
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				release, _, err := limiter.acquire(context.Background(), class)
				if err != nil {
					t.Errorf("acquire 失败: %v", err)
					return
				}
				now := atomic.AddInt64(&current, 1)
				for {
					old := atomic.LoadInt64(&peak)
					if now <= old || atomic.CompareAndSwapInt64(&peak, old, now) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt64(&current, -1)
				release()
			}()
		}
		wg.Wait()
		return atomic.LoadInt64(&peak)
	}

	if peak := run(classWrite, 20); peak > maxUpstreamWriteConcurrency {
		t.Fatalf("写并发峰值 %d 超过上限 %d", peak, maxUpstreamWriteConcurrency)
	}
	if peak := run(classRead, 30); peak > maxUpstreamReadConcurrency {
		t.Fatalf("读并发峰值 %d 超过上限 %d", peak, maxUpstreamReadConcurrency)
	}
}

// TestConnectionLimiterReturnsThrottledOnContextTimeout 等待中 ctx 到期必须返回 ErrUpstreamThrottled。
func TestConnectionLimiterReturnsThrottledOnContextTimeout(t *testing.T) {
	frozen := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	frozenNow := func() time.Time { return frozen }
	limiter := newConnectionLimiter("test-ctx", frozenNow, sleepWithContext)

	for i := 0; i < maxUpstreamRequestsPerMinute; i++ {
		release, _, err := limiter.acquire(context.Background(), classRead)
		if err != nil {
			t.Fatalf("预热第 %d 次失败: %v", i+1, err)
		}
		release()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := limiter.acquire(ctx, classRead)
	if !errors.Is(err, ErrUpstreamThrottled) {
		t.Fatalf("期望 ErrUpstreamThrottled，实际 %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("应在 ctx 超时后立即返回，实际耗时 %v", elapsed)
	}
}

// TestClassifyUpstreamRequest 只有创建/取消订单算写请求。
func TestClassifyUpstreamRequest(t *testing.T) {
	cases := []struct {
		method, path string
		want         requestClass
	}{
		{"POST", "/api/v1/upstream/orders", classWrite},
		{"POST", "/api/v1/upstream/orders/456/cancel", classWrite},
		{"GET", "/api/v1/upstream/orders/456", classRead},
		{"GET", "/api/v1/upstream/products?page=1", classRead},
		{"POST", "/api/v1/upstream/ping", classRead},
	}
	for _, tc := range cases {
		if got := classifyUpstreamRequest(tc.method, tc.path); got != tc.want {
			t.Fatalf("classify(%s %s) = %v，期望 %v", tc.method, tc.path, got, tc.want)
		}
	}
}

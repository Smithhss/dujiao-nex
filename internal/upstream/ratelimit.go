package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/logger"
)

// 上游文档 §11 的客户端限流约束：
//   - 单个凭证总请求 ≤ 60 次/分钟
//   - 写请求并发 ≤ 2
//   - 查询请求并发 ≤ 8
//   - 创建订单 ≤ 5 次/10 秒
const (
	maxUpstreamRequestsPerMinute = 60
	maxUpstreamWriteConcurrency  = 2
	maxUpstreamOrdersPer10s      = 5
	maxUpstreamReadConcurrency   = 8

	upstreamRateLimitWarnWait    = 2 * time.Second
	upstreamRateLimitDisabledEnv = "UPSTREAM_RATE_LIMIT_DISABLED"
)

// ErrUpstreamThrottled 本地限速等待超过调用方超时。
// 语义上属于"可重试"：调用方应保持幂等参数并稍后重试。
var ErrUpstreamThrottled = errors.New("upstream request throttled locally")

type requestClass int

const (
	classRead requestClass = iota
	classWrite
)

func (c requestClass) String() string {
	if c == classWrite {
		return "write"
	}
	return "read"
}

// classifyUpstreamRequest 按 method + path 判定读/写类别。
// 只有创建订单与取消订单属于写；ping 等虽然用 POST 但按读处理。
func classifyUpstreamRequest(method, path string) requestClass {
	m := strings.ToUpper(strings.TrimSpace(method))
	p := path
	if idx := strings.IndexByte(p, '?'); idx >= 0 {
		p = p[:idx]
	}
	if m == http.MethodPost && strings.HasPrefix(p, "/api/v1/upstream/orders") {
		return classWrite
	}
	return classRead
}

// slidingWindow 实现"任意 interval 内最多 limit 次"的滑动窗口。
type slidingWindow struct {
	mu       sync.Mutex
	events   []time.Time
	limit    int
	interval time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
}

func newSlidingWindow(limit int, interval time.Duration, now func() time.Time, sleep func(context.Context, time.Duration) error) *slidingWindow {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepWithContext
	}
	return &slidingWindow{limit: limit, interval: interval, now: now, sleep: sleep}
}

// acquire 占一个窗口槽位；窗口满时按最老事件到期时间等待，返回累计等待时长。
func (w *slidingWindow) acquire(ctx context.Context) (time.Duration, error) {
	var waited time.Duration
	for {
		w.mu.Lock()
		now := w.now()
		cutoff := now.Add(-w.interval)
		kept := w.events[:0]
		for _, at := range w.events {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		w.events = kept

		if len(w.events) < w.limit {
			w.events = append(w.events, now)
			w.mu.Unlock()
			return waited, nil
		}

		waitFor := w.events[0].Add(w.interval).Sub(now)
		w.mu.Unlock()
		if waitFor <= 0 {
			waitFor = time.Millisecond
		}
		if err := w.sleep(ctx, waitFor); err != nil {
			return waited, err
		}
		waited += waitFor
	}
}

// semaphore 以带缓冲 channel 实现并发上限。
type semaphore chan struct{}

func newSemaphore(n int) semaphore { return make(semaphore, n) }

func (s semaphore) acquire(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s semaphore) release() { <-s }

// connectionLimiter 是单个上游凭证共享的一组限速器。
type connectionLimiter struct {
	fingerprint string
	disabled    bool
	global      *slidingWindow
	orders      *slidingWindow
	writes      semaphore
	reads       semaphore
}

func newConnectionLimiter(fingerprint string, now func() time.Time, sleep func(context.Context, time.Duration) error) *connectionLimiter {
	return &connectionLimiter{
		fingerprint: fingerprint,
		global:      newSlidingWindow(maxUpstreamRequestsPerMinute, time.Minute, now, sleep),
		orders:      newSlidingWindow(maxUpstreamOrdersPer10s, 10*time.Second, now, sleep),
		writes:      newSemaphore(maxUpstreamWriteConcurrency),
		reads:       newSemaphore(maxUpstreamReadConcurrency),
	}
}

// acquire 取得一次请求所需的全部配额与并发槽，返回释放函数与累计等待时长。
// 注意：全局窗口槽位不随释放回滚，代表该次配额已使用；ctx 超时返回 ErrUpstreamThrottled。
func (l *connectionLimiter) acquire(ctx context.Context, class requestClass) (func(), time.Duration, error) {
	if l.disabled {
		return func() {}, 0, nil
	}

	var waited time.Duration
	w, err := l.global.acquire(ctx)
	waited += w
	if err != nil {
		return nil, waited, ErrUpstreamThrottled
	}

	sem := l.reads
	if class == classWrite {
		sem = l.writes
	}
	if err := sem.acquire(ctx); err != nil {
		return nil, waited, ErrUpstreamThrottled
	}
	release := func() { sem.release() }

	if class == classWrite {
		w, err := l.orders.acquire(ctx)
		waited += w
		if err != nil {
			release()
			return nil, waited, ErrUpstreamThrottled
		}
	}

	if waited >= upstreamRateLimitWarnWait {
		logger.Warnw("upstream_rate_limited_wait",
			"key", l.fingerprint,
			"class", class.String(),
			"wait_ms", waited.Milliseconds())
	}
	return release, waited, nil
}

// sleepWithContext 可被 ctx 取消的等待。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// limiterRegistry 让同一凭证的所有适配器实例共享限速状态（适配器每次调用都会新建）。
var limiterRegistry sync.Map

// limiterKey 用 baseURL + apiKey 的哈希前 16 位做指纹，避免明文 secret 进入日志。
func limiterKey(baseURL, apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(baseURL) + "\n" + strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])[:16]
}

func limiterFor(baseURL, apiKey string) *connectionLimiter {
	key := limiterKey(baseURL, apiKey)
	if existing, ok := limiterRegistry.Load(key); ok {
		return existing.(*connectionLimiter)
	}
	created := newConnectionLimiter(key, time.Now, sleepWithContext)
	created.disabled = upstreamRateLimitDisabled()
	actual, _ := limiterRegistry.LoadOrStore(key, created)
	return actual.(*connectionLimiter)
}

// upstreamRateLimitDisabled 仅用于本地排障/测试：UPSTREAM_RATE_LIMIT_DISABLED=1 时关闭限速。
func upstreamRateLimitDisabled() bool {
	value := strings.TrimSpace(os.Getenv(upstreamRateLimitDisabledEnv))
	return value == "1" || strings.EqualFold(value, "true")
}

package contract

import "errors"

var (
	ErrNotFound           = errors.New("procurement order not found")
	ErrExists             = errors.New("procurement order already exists")
	ErrStatusInvalid      = errors.New("procurement order status invalid")
	ErrOrderNotFound      = errors.New("order not found")
	ErrConnectionNotFound = errors.New("site connection not found")

	// ErrUpstreamAuthFailed 上游鉴权失败（401/403）：调用方不得自动重试。
	ErrUpstreamAuthFailed = errors.New("upstream auth failed")
)

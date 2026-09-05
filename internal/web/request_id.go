package web

import (
	"fmt"
	"sync/atomic"
	"time"
)

var fallbackRequestID atomic.Uint64

func newRequestID() string {
	if token, err := randomToken(); err == nil {
		return "web-" + token
	}
	// The request can still be correlated if the operating system random source
	// is temporarily unavailable.
	return fmt.Sprintf("web-%x-%x", time.Now().UnixNano(), fallbackRequestID.Add(1))
}

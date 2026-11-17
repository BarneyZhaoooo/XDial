package mobile

import (
	"encoding/json"
	"sync"
)

// ConnectionController owns connection intent, epochs and a bounded recovery
// budget. Hosts provide opaque OS network identities and execute one attempt at
// a time. It never selects interfaces, resolves names or opens sockets.
type ConnectionController struct {
	mu       sync.Mutex
	desired  bool
	network  string
	epoch    int64
	phase    string
	failures int
	retryAt  int64
}

type connectionState struct {
	Desired bool   `json:"desired"`
	Epoch   int64  `json:"epoch"`
	Phase   string `json:"phase"`
	RetryAt int64  `json:"retry_at_ms"`
}

func NewConnectionController() *ConnectionController {
	return &ConnectionController{phase: "disconnected"}
}
func (c *ConnectionController) RequestConnection(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.desired == enabled {
		return
	}
	c.desired = enabled
	c.epoch++
	c.failures = 0
	c.retryAt = 0
	c.ready()
}
func (c *ConnectionController) ready() {
	if !c.desired {
		c.phase = "disconnected"
	} else if c.network == "" {
		c.phase = "waiting_network"
	} else {
		c.phase = "ready"
	}
}

// An empty identity means unavailable. A changed LinkProperties snapshot must
// have a new opaque identity even when the Android Network handle is unchanged.
func (c *ConnectionController) ObserveUnderlay(identity string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.network == identity {
		return
	}
	c.network = identity
	c.epoch++
	c.failures = 0
	c.retryAt = 0
	c.ready()
}

// NextAttempt leases the current epoch once; subsequent calls return zero until
// completion, a new network, or the retry deadline. nowMS must be monotonic.
func (c *ConnectionController) NextAttempt(nowMS int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.desired || c.network == "" {
		return 0
	}
	if c.phase != "ready" && !(c.phase == "backoff" && nowMS >= c.retryAt) {
		return 0
	}
	c.epoch++ // each lease also invalidates callbacks from earlier retry attempts
	c.phase = "connecting"
	return c.epoch
}

// Stale completions cannot resurrect a stopped connection or affect a newer
// network. Authentication, permissions and invalid configuration are terminal.
func (c *ConnectionController) CompleteAttempt(epoch int64, success, retryable bool, nowMS int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.epoch || !c.desired || c.network == "" || (c.phase != "connecting" && c.phase != "connected") {
		return
	}
	if success {
		c.phase = "connected"
		c.failures = 0
		c.retryAt = 0
		return
	}
	c.failures++
	if !retryable || c.failures > 4 {
		if !retryable {
			c.desired = false
		}
		c.phase = "failed"
		c.retryAt = 0
		return
	}
	c.phase = "backoff"
	c.retryAt = nowMS + int64(1000<<(c.failures-1))
}
func (c *ConnectionController) Snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, _ := json.Marshal(connectionState{c.desired, c.epoch, c.phase, c.retryAt})
	return string(data)
}

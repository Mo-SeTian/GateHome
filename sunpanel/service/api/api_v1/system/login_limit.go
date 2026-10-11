package system

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"sun-panel/lib/cmn"
	"sync"
	"time"
)

type loginBucket struct {
	start time.Time
	count int
}
type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]loginBucket
}

var loginAttempts = &loginLimiter{buckets: map[string]loginBucket{}}
var loginWorkers = make(chan struct{}, 4)
var dummyLoginHash, _ = cmn.HashPassword("UNUSED_LOGIN_TIMING_PASSWORD")

func (l *loginLimiter) allow(ip, account string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, value := range l.buckets {
		if now.Sub(value.start) >= time.Minute {
			delete(l.buckets, key)
		}
	}
	keys := []string{"ip:" + ip, "account:" + account}
	limits := []int{20, 10}
	missing := 0
	for i, key := range keys {
		value, exists := l.buckets[key]
		if !exists {
			missing++
		}
		if value.count >= limits[i] {
			return false
		}
	}
	if len(l.buckets)+missing > 4096 {
		return false
	}
	for _, key := range keys {
		value := l.buckets[key]
		if value.count == 0 {
			value.start = now
		}
		value.count++
		l.buckets[key] = value
	}
	return true
}
func loginBusy(c *gin.Context) {
	c.Header("Retry-After", "60")
	c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "登录尝试过多，请稍后重试"})
}

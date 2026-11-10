package gateway

import "time"

// Make room for one new session without evicting a newer active session first.
func pruneSessions(sessions map[[32]byte]time.Time, limit int, now time.Time) {
	var oldest [32]byte
	var earliest time.Time
	for key, expiry := range sessions {
		if !now.Before(expiry) {
			delete(sessions, key)
			continue
		}
		if earliest.IsZero() || expiry.Before(earliest) {
			oldest, earliest = key, expiry
		}
	}
	if len(sessions) >= limit {
		delete(sessions, oldest)
	}
}

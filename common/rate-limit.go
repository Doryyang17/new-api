package common

import (
	"sync"
	"time"
)

type InMemoryRateLimiter struct {
	store              map[string]*[]rateLimitEntry
	mutex              sync.Mutex
	expirationDuration time.Duration
	nextReservationID  uint64
}

type rateLimitEntry struct {
	timestamp     int64
	reservationID uint64
}

func pruneRateLimitQueue(queue *[]rateLimitEntry, now int64, duration int64) {
	for len(*queue) > 0 && now-(*queue)[0].timestamp >= duration {
		*queue = (*queue)[1:]
	}
}

func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	if l.store == nil {
		l.mutex.Lock()
		if l.store == nil {
			l.store = make(map[string]*[]rateLimitEntry)
			l.expirationDuration = expirationDuration
			if expirationDuration > 0 {
				go l.clearExpiredItems()
			}
		}
		l.mutex.Unlock()
	}
}

func (l *InMemoryRateLimiter) clearExpiredItems() {
	for {
		time.Sleep(l.expirationDuration)
		l.mutex.Lock()
		now := time.Now().Unix()
		for key := range l.store {
			queue := l.store[key]
			size := len(*queue)
			if size == 0 || now-(*queue)[size-1].timestamp > int64(l.expirationDuration.Seconds()) {
				delete(l.store, key)
			}
		}
		l.mutex.Unlock()
	}
}

// Request parameter duration's unit is seconds
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	_, allowed := l.Reserve(key, maxRequestNum, duration)
	return allowed
}

func (l *InMemoryRateLimiter) Reserve(key string, maxRequestNum int, duration int64) (uint64, bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if maxRequestNum <= 0 {
		return 0, true
	}
	// [old <-- new]
	queue, ok := l.store[key]
	now := time.Now().Unix()
	if ok {
		pruneRateLimitQueue(queue, now, duration)
		if len(*queue) < maxRequestNum {
			l.nextReservationID++
			reservationID := l.nextReservationID
			*queue = append(*queue, rateLimitEntry{timestamp: now, reservationID: reservationID})
			return reservationID, true
		}
		return 0, false
	} else {
		s := make([]rateLimitEntry, 0, maxRequestNum)
		l.store[key] = &s
		l.nextReservationID++
		reservationID := l.nextReservationID
		*(l.store[key]) = append(*(l.store[key]), rateLimitEntry{timestamp: now, reservationID: reservationID})
		return reservationID, true
	}
}

func (l *InMemoryRateLimiter) Rollback(key string, reservationID uint64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if reservationID == 0 {
		return false
	}
	queue, ok := l.store[key]
	if !ok {
		return false
	}
	for index, entry := range *queue {
		if entry.reservationID != reservationID {
			continue
		}
		*queue = append((*queue)[:index], (*queue)[index+1:]...)
		if len(*queue) == 0 {
			delete(l.store, key)
		}
		return true
	}
	return false
}

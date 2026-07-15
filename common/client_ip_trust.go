package common

import "sync/atomic"

var clientIPTrustConfigured atomic.Bool

func SetClientIPTrustConfigured(configured bool) {
	clientIPTrustConfigured.Store(configured)
}

func IsClientIPTrustConfigured() bool {
	return clientIPTrustConfigured.Load()
}

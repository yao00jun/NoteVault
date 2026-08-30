//go:build !windows || server

package platform

func AcquireProcessLock(_ string) (release func(), acquired bool) {
	return func() {}, true
}

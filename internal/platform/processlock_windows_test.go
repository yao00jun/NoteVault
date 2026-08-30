//go:build windows && !server

package platform

import (
	"fmt"
	"os"
	"testing"
)

func TestAcquireProcessLockAllowsOnlyOneOwner(t *testing.T) {
	uniqueID := fmt.Sprintf("test-%d", os.Getpid())
	releaseFirst, acquiredFirst := AcquireProcessLock(uniqueID)
	if !acquiredFirst {
		t.Fatal("first process lock acquisition should succeed")
	}

	releaseSecond, acquiredSecond := AcquireProcessLock(uniqueID)
	releaseSecond()
	if acquiredSecond {
		t.Fatal("second process lock acquisition should fail")
	}

	releaseFirst()
	releaseThird, acquiredThird := AcquireProcessLock(uniqueID)
	releaseThird()
	if !acquiredThird {
		t.Fatal("lock should be acquirable again after release")
	}
}

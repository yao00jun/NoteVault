package service

import (
	"fmt"
	"sync"
	"testing"
)

func TestForEachFileBounded_ProcessesEachFileExactlyOnce(t *testing.T) {
	const n = 500
	files := make([]string, n)
	for i := range files {
		files[i] = fmt.Sprintf("f%03d.md", i)
	}

	var mu sync.Mutex
	seen := make(map[string]int, n)
	active := 0
	peak := 0

	forEachFileBounded(files, func(fp string) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()

		mu.Lock()
		seen[fp]++
		mu.Unlock()

		mu.Lock()
		active--
		mu.Unlock()
	})

	if len(seen) != n {
		t.Fatalf("processed %d distinct files, want %d", len(seen), n)
	}
	for _, f := range files {
		if seen[f] != 1 {
			t.Fatalf("file %s processed %d times, want exactly 1", f, seen[f])
		}
	}
	if peak == 0 {
		t.Fatal("peak concurrency should be > 0")
	}
	if cap := maxConcurrentFileWorkers(n); peak > cap {
		t.Errorf("peak concurrency %d exceeds cap %d", peak, cap)
	}
}

func TestForEachFileBounded_EdgeCases(t *testing.T) {
	// 空列表：不应 panic，也不应调用 work
	forEachFileBounded(nil, func(string) {
		t.Error("work should not be called for empty file list")
	})

	// 单文件：退化到串行路径
	calls := 0
	forEachFileBounded([]string{"only.md"}, func(fp string) {
		calls++
		if fp != "only.md" {
			t.Errorf("unexpected file %q", fp)
		}
	})
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}

func TestMaxConcurrentFileWorkers_CappedByFileCount(t *testing.T) {
	// 文件数小于 CPU 核数时，worker 数不应超过文件数（否则会起多余的 goroutine）
	if got := maxConcurrentFileWorkers(2); got > 2 {
		t.Errorf("maxConcurrentFileWorkers(2) = %d, want <= 2", got)
	}
	if got := maxConcurrentFileWorkers(0); got != 0 {
		t.Errorf("maxConcurrentFileWorkers(0) = %d, want 0", got)
	}
}

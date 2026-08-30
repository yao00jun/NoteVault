package service

import (
	"runtime"
	"sync"
)

// maxConcurrentFileWorkers 返回文件扫描的并发上限。
// 扫描是 I/O + CPU 混合型任务（ReadFile + 正则），超过 CPU 核数的并发不会更快，
// 只会增加 goroutine 调度开销与瞬时内存峰值（每个并发各持一份文件缓冲区）。
func maxConcurrentFileWorkers(fileCount int) int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > fileCount {
		workers = fileCount
	}
	return workers
}

// forEachFileBounded 以有界并发对文件列表执行 work，全部完成后才返回。
//
// 旧实现是「每文件一个 goroutine」：5000 个文件会瞬间起 5000 个 goroutine，
// 而真正的临界区（结果 map/slice 追加）本身是串行的——多出的并发既无吞吐收益，
// 又会放大调度开销与瞬时内存峰值。这里改为固定 NumCPU 量级的 worker 池。
//
// work 内部对共享状态的写仍需调用方自行加锁（本函数不代为加锁）。
func forEachFileBounded(files []string, work func(filePath string)) {
	workers := maxConcurrentFileWorkers(len(files))
	if workers <= 1 {
		for _, f := range files {
			work(f)
		}
		return
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for fp := range jobs {
				work(fp)
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
}

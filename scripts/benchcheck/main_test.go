package main

import (
	"math"
	"strings"
	"testing"
)

func TestParse_HandlesUnitsAndStripsGOMAXPROCS(t *testing.T) {
	out := `goos: linux
goarch: amd64
pkg: github.com/notevault/notevault/internal/service
cpu: Intel(R) Xeon(R)
BenchmarkTokenize-16          1000000        951 ns/op
BenchmarkBuildIndex-4             100       1.50 ms/op     8192 B/op        3 allocs/op
BenchmarkSerialize-8              10  1500000000 ns/op       1.20 MB/op       42 allocs/op
PASS
ok  	github.com/notevault/notevault/internal/service	2.5s
`
	got, err := parse(strings.NewReader(out), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.goos != "linux" {
		t.Errorf("应从表头解析出 goos=linux，实际 %q", got.goos)
	}
	if len(got.benchmarks) != 3 {
		t.Fatalf("期望解析出 3 个基准，实际 %d: %+v", len(got.benchmarks), got.benchmarks)
	}
	// -N 是 GOMAXPROCS，必须剥掉，否则本机与 runner 的同一基准对不上
	if got.benchmarks[0].name != "BenchmarkBuildIndex" || got.benchmarks[1].name != "BenchmarkSerialize" || got.benchmarks[2].name != "BenchmarkTokenize" {
		t.Errorf("基准名（剥 GOMAXPROCS 后）不符: %+v", got.benchmarks)
	}
	if got.benchmarks[0].min.nsPerOp != 1.5e6 {
		t.Errorf("ms → ns 换算错误: %v", got.benchmarks[0].min.nsPerOp)
	}
	if got.benchmarks[1].min.nsPerOp != 1.5e9 {
		t.Errorf("大纳秒值解析错误: %v", got.benchmarks[1].min.nsPerOp)
	}
	if got.benchmarks[1].min.bytes != 1.2e6 || got.benchmarks[1].min.allocs != 42 {
		t.Errorf("MB / allocs 解析错误: %+v", got.benchmarks[1].min)
	}
	if got.benchmarks[2].min.bytes != 0 || got.benchmarks[2].min.allocs != 0 {
		t.Errorf("没有 -benchmem 的行不应凭空产生内存指标: %+v", got.benchmarks[2].min)
	}
}

func TestParse_NoHeaderMeansUnknownPlatform(t *testing.T) {
	got, err := parse(strings.NewReader("BenchmarkX-8   100   5 ns/op\n"), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.goos != "" {
		t.Errorf("没有表头时 goos 应为空串，实际 %q", got.goos)
	}
}

func TestParse_CustomMetricsDoNotHideMemoryMetrics(t *testing.T) {
	// b.ReportMetric 的自定义指标会混在标准指标之间。按固定位置解析时，
	// 内存两段会被静默解析成 0——而 0 意味着「基线没采到，不判定」，门禁无声失效。
	out := `BenchmarkIndexBuild/5000-16   100   1234567 ns/op   456 ns/doc   123456 B/op   1234 allocs/op   3 hits
BenchmarkSearch/bm25/100/rare-16   1000   950 ns/op   512 B/op   7 allocs/op   1 hits
`
	got, err := parse(strings.NewReader(out), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	first := got.benchmarks[0].min
	if first.nsPerOp != 1234567 || first.bytes != 123456 || first.allocs != 1234 {
		t.Errorf("自定义指标在前：标准指标应全部解析到，实际 %+v", first)
	}
	second := got.benchmarks[1].min
	if second.nsPerOp != 950 || second.bytes != 512 || second.allocs != 7 {
		t.Errorf("自定义指标在后：标准指标应全部解析到，实际 %+v", second)
	}
}

func TestParse_LineWithoutNsPerOpIsAnError(t *testing.T) {
	// 认不出时间单位时不能把耗时当成 0 放过去
	for name, out := range map[string]string{
		"未知时间单位":  "BenchmarkX-8   100   5 fortnights/op\n",
		"只有迭代次数":  "BenchmarkX-8   100\n",
		"指标值不是数字": "BenchmarkX-8   100   abc ns/op\n",
	} {
		if _, err := parse(strings.NewReader(out), name); err == nil {
			t.Errorf("%s：期望报错，实际通过", name)
		}
	}
}

func TestParse_TakesMinimumAcrossRepeats(t *testing.T) {
	// -count=3：同一基准三行，噪声会让某一行偏慢，必须取最小值
	out := `BenchmarkX-8   100   1200 ns/op   64 B/op   2 allocs/op
BenchmarkX-8   100   1800 ns/op   64 B/op   2 allocs/op
BenchmarkX-8   100    950 ns/op   80 B/op   2 allocs/op
`
	got, err := parse(strings.NewReader(out), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.benchmarks) != 1 {
		t.Fatalf("应聚合成 1 个基准，实际 %d", len(got.benchmarks))
	}
	if got.benchmarks[0].min.nsPerOp != 950 || got.benchmarks[0].min.bytes != 64 || got.benchmarks[0].min.allocs != 2 {
		t.Errorf("应取各项最小值，实际 %+v", got.benchmarks[0].min)
	}
}

func TestParse_DoesNotSplitDashedSubbenchmarkNames(t *testing.T) {
	// 子基准名里带数字段时，只剥最后一段 GOMAXPROCS
	out := "BenchmarkIndex/n-1000-docs-16   100   5 µs/op   10 B/op   1 allocs/op\n"
	got, err := parse(strings.NewReader(out), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.benchmarks[0].name != "BenchmarkIndex/n-1000-docs" {
		t.Errorf("基准名不符: %q", got.benchmarks[0].name)
	}
	if math.Abs(got.benchmarks[0].min.nsPerOp-5e3) > 1e-9 {
		t.Errorf("µs → ns 换算错误: %v", got.benchmarks[0].min.nsPerOp)
	}
}

func TestParse_EmptyOutputIsAnError(t *testing.T) {
	// 基准没跑起来（比如包编译失败）时不能当成「没有回归」放过去
	for name, out := range map[string]string{
		"空输出":    "",
		"只有表头":   "goos: linux\ncpu: x\nPASS\n",
		"格式不符":   "SomethingElse-8   100   5 ns/op\n",
		"未知时间单位": "BenchmarkX-8   100   5 fortnights/op\n",
	} {
		if _, err := parse(strings.NewReader(out), name); err == nil {
			t.Errorf("%s：期望报错，实际通过", name)
		}
	}
}

func TestExceeds(t *testing.T) {
	cases := []struct {
		name      string
		base      float64
		current   float64
		tolerance float64
		want      bool
	}{
		{"远小于阈值", 100, 150, 3, false},
		{"恰好等于阈值不算超出", 100, 300, 3, false},
		{"刚超过阈值", 100, 300.01, 3, true},
		{"算法级退化", 100, 10000, 3, true},
		{"基线没采到不判", 0, 10000, 3, false},
		{"当前没采到不判", 100, 0, 3, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := exceeds(c.base, c.current, c.tolerance); got != c.want {
				t.Errorf("exceeds(%v, %v, %v) = %v，期望 %v", c.base, c.current, c.tolerance, got, c.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		ns   float64
		want string
	}{
		{951, "951 ns"},
		{1500, "1.5 µs"},
		{1.5e6, "1.50 ms"},
		{2.5e9, "2.50 s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.ns); got != c.want {
			t.Errorf("formatDuration(%v) = %q，期望 %q", c.ns, got, c.want)
		}
	}
	if got := formatBytes(8192); got != "8.2 KB" {
		t.Errorf("formatBytes(8192) = %q", got)
	}
	if ratio(0, 5) != "-" || ratio(100, 250) != "2.50x" {
		t.Errorf("ratio 输出不符: %q %q", ratio(0, 5), ratio(100, 250))
	}
}

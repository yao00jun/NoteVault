// Command benchcheck 把一次 `go test -bench` 的输出与仓库里的基线对比，
// 只在「超出容忍度」时以非零码退出，供 CI 做性能回归门禁。
//
// 用法：
//
//	go test ./internal/service/ -run '^$' -bench . -benchmem -count=3 > bench.txt
//	go run ./scripts/benchcheck -baseline benchmarks/baseline.txt -current bench.txt
//
// 设计取舍（为什么阈值这么宽）：
//
// 托管 runner 的计时抖动很大（±50% 常见，偶发数倍），窄阈值必然误红，
// 而误红的门禁很快就会被当成噪声关掉。因此两类指标分治：
//
//   - 耗时（ns/op）：同一基准多次测量取**最小值**（最小值对抖动最稳健），
//     只有超过基线 timeTolerance 倍才判失败。默认 3 倍——足以放过机器差异
//     与噪声，又能抓住 O(n) 变 O(n²) 这类算法级退化（通常是 10 倍以上）。
//   - 内存（B/op、allocs/op）：分配次数在给定输入下基本是确定性的，
//     不随机器快慢变化，所以用更紧的阈值。
//
// 基线里没有的新基准只报告不判失败；当前输出里缺失的基准给出警告
// （被改名或删掉时应同步更新基线）。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// timeUnits 把各时间单位换算成纳秒。
var timeUnits = map[string]float64{
	"ns": 1,
	"us": 1e3, "µs": 1e3, "μs": 1e3,
	"ms": 1e6,
	"s":  1e9,
}

// byteUnits 把各字节单位换算成字节。testing 包按 1000 进位输出（KB/MB/GB），
// 兼容二进制单位只是为了将来输出格式变化时不至于解析失败。
var byteUnits = map[string]float64{
	"B":  1,
	"KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
}

// gomaxprocsSuffix 匹配基准名末尾的 GOMAXPROCS 段（testing 包自动追加的 `-N`）。
var gomaxprocsSuffix = regexp.MustCompile(`-\d+$`)

// parseLine 解析 testing 包的一行基准输出：
//
//	BenchmarkName-8   1000   951.2 ns/op   128 B/op   2 allocs/op   99 ns/doc
//
// 名称后是迭代次数（裸数字），再往后全是「值 单位」成对出现的指标。
// 按成对方式解析而不是认固定位置：b.ReportMetric 的自定义指标（ns/doc、hits……）
// 会混在标准指标之间，按位置取会把内存两段静默解析成 0——而 0 意味着「不判定」，
// 等于门禁无声失效。认不出的单位（自定义指标）忽略；内存指标可以不出现
// （没带 -benchmem 时）。没有 ns/op 的行视为格式不符。
func parseLine(line string) (string, metrics, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", metrics{}, fmt.Errorf("基准行字段不足：%s", line)
	}
	// 基准名里的 -N 后缀是 GOMAXPROCS，本机与 runner 的核数不同，必须剥掉再比
	name := gomaxprocsSuffix.ReplaceAllString(fields[0], "")

	i := 1
	// 迭代次数：紧跟名称的裸数字，没有单位
	if _, err := strconv.ParseFloat(fields[i], 64); err == nil {
		i++
	}
	var m metrics
	hasNs := false
	for ; i+1 < len(fields); i += 2 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return "", metrics{}, fmt.Errorf("指标值解析失败：%s", line)
		}
		unit := fields[i+1]
		// testing 包的单位带 `/op` 后缀（ns/op、B/op、allocs/op），也允许裸单位
		base := strings.TrimSuffix(unit, "/op")
		timeScale, isTime := timeUnits[base]
		byteScale, isBytes := byteUnits[base]
		switch {
		case isTime:
			m.nsPerOp, hasNs = value*timeScale, true
		case isBytes:
			m.bytes = value * byteScale
		case base == "allocs":
			m.allocs = value
		default:
			// 自定义指标（hits、MB/s、truncated……），不参与判定
		}
	}
	if !hasNs {
		return "", metrics{}, fmt.Errorf("基准行缺少 ns/op：%s", line)
	}
	return name, m, nil
}

// metrics 是单个基准在一行输出里的全部指标（已换算为 ns / B）。
type metrics struct {
	nsPerOp float64
	bytes   float64
	allocs  float64
}

// aggregate 保存同一基准多次测量的最小值。
type aggregate struct {
	name string
	min  metrics
}

func (a *aggregate) observe(m metrics) {
	// 首次观察时 min 全为 0，直接取当前值；之后逐项取小。
	if a.min.nsPerOp == 0 || (m.nsPerOp > 0 && m.nsPerOp < a.min.nsPerOp) {
		a.min.nsPerOp = m.nsPerOp
	}
	if m.bytes > 0 && (a.min.bytes == 0 || m.bytes < a.min.bytes) {
		a.min.bytes = m.bytes
	}
	if m.allocs > 0 && (a.min.allocs == 0 || m.allocs < a.min.allocs) {
		a.min.allocs = m.allocs
	}
}

// benchResult 是一份基准输出的全部内容。
type benchResult struct {
	// benchmarks 按名称排序的最小值列表
	benchmarks []aggregate
	// goos 取自输出表头的 `goos:` 行；没有表头时为空串
	goos string
}

// parseFile 读取 go test -bench 的输出文件。
func parseFile(path string) (benchResult, error) {
	f, err := os.Open(path) //nolint:gosec // 路径由 CI/开发者显式传入
	if err != nil {
		return benchResult{}, err
	}
	defer func() { _ = f.Close() }()
	return parse(f, path)
}

// goosLine 匹配输出表头里的平台行。
var goosLine = regexp.MustCompile(`^goos:\s*(\S+)`)

// parse 读取 go test -bench 的输出，返回按名称排序的最小值列表与平台。
// 非 benchmark 行（PASS、ok、日志）一律跳过。
func parse(r io.Reader, label string) (benchResult, error) {
	byName := map[string]*aggregate{}
	scanner := bufio.NewScanner(r)
	// 基准输出行很短，但 -benchmem 下带自定义指标时可能较长，放宽上限避免静默截断
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	parsed := 0
	goos := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if platform := goosLine.FindStringSubmatch(line); platform != nil {
			goos = platform[1]
			continue
		}
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		name, m, err := parseLine(line)
		if err != nil {
			return benchResult{}, err
		}
		agg, exists := byName[name]
		if !exists {
			agg = &aggregate{name: name}
			byName[name] = agg
		}
		agg.observe(m)
		parsed++
	}
	if err := scanner.Err(); err != nil {
		return benchResult{}, err
	}
	if parsed == 0 {
		return benchResult{}, fmt.Errorf("%s 中没有解析到任何基准行（格式不符或基准根本没跑）", label)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]aggregate, 0, len(names))
	for _, name := range names {
		result = append(result, *byName[name])
	}
	return benchResult{benchmarks: result, goos: goos}, nil
}

// exceeds 判断 current 是否超过 base 的 tolerance 倍。
// 基线值为 0（该指标当时没采到）时不判——没有参照物就没有回归可言。
func exceeds(base, current, tolerance float64) bool {
	if base <= 0 || current <= 0 {
		return false
	}
	return current > base*tolerance
}

func ratio(base, current float64) string {
	if base <= 0 || current <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2fx", current/base)
}

func formatDuration(ns float64) string {
	switch {
	case ns >= 1e9:
		return fmt.Sprintf("%.2f s", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.2f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.1f µs", ns/1e3)
	default:
		return fmt.Sprintf("%.0f ns", ns)
	}
}

func formatBytes(b float64) string {
	switch {
	case b >= 1e6:
		return fmt.Sprintf("%.2f MB", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1f KB", b/1e3)
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}

func main() {
	baselinePath := flag.String("baseline", "benchmarks/baseline.txt", "基线输出文件（go test -bench 的原始输出）")
	currentPath := flag.String("current", "", "本次输出文件；留空或 - 表示从标准输入读")
	timeTolerance := flag.Float64("time-tolerance", 3, "耗时（ns/op）允许的倍数，超过即失败")
	allocTolerance := flag.Float64("alloc-tolerance", 1.5, "分配次数（allocs/op）允许的倍数，超过即失败")
	bytesTolerance := flag.Float64("bytes-tolerance", 2, "分配字节（B/op）允许的倍数，超过即失败")
	markdownPath := flag.String("markdown", "", "把 Markdown 对比表追加写到这个文件（CI 里传 $GITHUB_STEP_SUMMARY）")
	flag.Parse()

	if *timeTolerance < 1 || *allocTolerance < 1 || *bytesTolerance < 1 {
		fmt.Fprintln(os.Stderr, "容忍度必须 ≥ 1")
		os.Exit(2)
	}
	currentReader, label := io.Reader(os.Stdin), "stdin"
	if *currentPath != "" && *currentPath != "-" {
		f, err := os.Open(*currentPath) //nolint:gosec // 路径由 CI/开发者显式传入
		if err != nil {
			fmt.Fprintf(os.Stderr, "打开本次结果失败: %v\n", err)
			os.Exit(2)
		}
		defer func() { _ = f.Close() }()
		currentReader, label = f, *currentPath
	}

	baseResult, err := parseFile(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取基线失败: %v\n", err)
		os.Exit(2)
	}
	currentResult, err := parse(currentReader, label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取本次结果失败: %v\n", err)
		os.Exit(2)
	}
	baseline, current := baseResult.benchmarks, currentResult.benchmarks

	// 内存指标（B/op、allocs/op）只在**同一操作系统**内才是确定性的：
	// Windows 的系统调用包装层（UTF-16 路径转换等）比 Linux 多几次分配，
	// 文件类基准跨平台硬比必然误红。基线平台与当前不同时内存指标只报告不判定，
	// 仅耗时参与门禁；用 CI 产物刷新一次基线后自动升级为全严格。
	memoryComparable := baseResult.goos == "" || currentResult.goos == "" || baseResult.goos == currentResult.goos
	if !memoryComparable {
		fmt.Fprintf(os.Stderr, "提示：基线平台（%s）与当前（%s）不同，内存指标只报告不判定——用本次输出刷新基线即可启用。\n",
			baseResult.goos, currentResult.goos)
	}

	baseByName := make(map[string]aggregate, len(baseline))
	for _, item := range baseline {
		baseByName[item.name] = item
	}
	currentByName := make(map[string]aggregate, len(current))
	for _, item := range current {
		currentByName[item.name] = item
	}

	var report strings.Builder
	fmt.Fprintf(&report, "耗时阈值 %.1fx · B/op 阈值 %.1fx · allocs/op 阈值 %.1fx（取多次测量的最小值）\n\n",
		*timeTolerance, *bytesTolerance, *allocTolerance)
	if !memoryComparable {
		fmt.Fprintf(&report, "> 基线平台 %s ≠ 当前 %s：内存指标仅报告，不参与判定。\n\n", baseResult.goos, currentResult.goos)
	}
	report.WriteString("| 基准 | ns/op 基线 → 当前 | B/op 基线 → 当前 | allocs/op 基线 → 当前 | 结论 |\n")
	report.WriteString("| :--- | ---: | ---: | ---: | :--- |\n")

	type breach struct {
		name   string
		metric string
		detail string
	}
	var breaches []breach
	var missing []string

	for _, cur := range current {
		base, ok := baseByName[cur.name]
		verdict := "OK"
		breachStart := len(breaches)
		if !ok {
			verdict = "NEW（基线里没有，只报告）"
		} else {
			if exceeds(base.min.nsPerOp, cur.min.nsPerOp, *timeTolerance) {
				breaches = append(breaches, breach{cur.name, "ns/op",
					fmt.Sprintf("%s → %s（%.2fx）", formatDuration(base.min.nsPerOp), formatDuration(cur.min.nsPerOp), cur.min.nsPerOp/base.min.nsPerOp)})
			}
			if memoryComparable && exceeds(base.min.bytes, cur.min.bytes, *bytesTolerance) {
				breaches = append(breaches, breach{cur.name, "B/op",
					fmt.Sprintf("%s → %s（%.2fx）", formatBytes(base.min.bytes), formatBytes(cur.min.bytes), cur.min.bytes/base.min.bytes)})
			}
			if memoryComparable && exceeds(base.min.allocs, cur.min.allocs, *allocTolerance) {
				breaches = append(breaches, breach{cur.name, "allocs/op",
					fmt.Sprintf("%.0f → %.0f（%.2fx）", base.min.allocs, cur.min.allocs, cur.min.allocs/base.min.allocs)})
			}
			if len(breaches) > breachStart {
				verdict = "REGRESSION"
			}
		}
		fmt.Fprintf(&report, "| `%s` | %s → %s (%s) | %s → %s (%s) | %.0f → %.0f (%s) | %s |\n",
			cur.name,
			formatDuration(base.min.nsPerOp), formatDuration(cur.min.nsPerOp), ratio(base.min.nsPerOp, cur.min.nsPerOp),
			formatBytes(base.min.bytes), formatBytes(cur.min.bytes), ratio(base.min.bytes, cur.min.bytes),
			base.min.allocs, cur.min.allocs, ratio(base.min.allocs, cur.min.allocs),
			verdict)
	}
	for _, base := range baseline {
		if _, ok := currentByName[base.name]; !ok {
			missing = append(missing, base.name)
		}
	}
	for _, name := range missing {
		fmt.Fprintf(&report, "| `%s` | 本次缺失 | - | - | 基准被改名或删除？请同步基线 |\n", name)
	}

	fmt.Print(report.String())

	if *markdownPath != "" {
		f, err := os.OpenFile(*markdownPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // 路径由 CI 显式传入（$GITHUB_STEP_SUMMARY）
		if err == nil {
			_, _ = f.WriteString(report.String())
			_ = f.Close()
		} else {
			fmt.Fprintf(os.Stderr, "写 Markdown 报告失败（不影响判定）: %v\n", err)
		}
	}

	for _, name := range missing {
		fmt.Fprintf(os.Stderr, "警告：基准 %s 在本次输出中缺失\n", name)
	}
	if len(breaches) > 0 {
		fmt.Fprintln(os.Stderr, "性能回归：")
		for _, b := range breaches {
			fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", b.name, b.metric, b.detail)
			fmt.Printf("::error title=性能回归::%s %s %s\n", b.name, b.metric, b.detail)
		}
		fmt.Fprintf(os.Stderr, "共 %d 项超出容忍度。\n", len(breaches))
		fmt.Fprintln(os.Stderr, "如果是预期的性能取舍：本地重跑基准并把结果写回 benchmarks/baseline.txt（做法见 benchmarks/README.md）。")
		os.Exit(1)
	}
}

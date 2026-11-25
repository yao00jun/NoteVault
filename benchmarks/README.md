# 性能基线与回归门禁

这个目录放性能基准的基线数据。CI 的 `Performance Benchmarks` job 拿本次测量与
对应平台的基线对比，超出容忍度才失败——目的是让「某次重构把 1ms 变 10ms」在合并前暴露，
而不是等用户反馈卡顿。

**基线按平台分文件，不可混用**：

| 文件 | 平台 | 谁在用 |
| :--- | :--- | :--- |
| `baseline.txt` | Windows 11 / Ryzen 5 7500F | 本地开发 |
| `baseline-linux.txt` | ubuntu-latest / Xeon Platinum 8573C（4 核） | CI 门禁 |

同一份代码在两平台上的比值落在 **0.02x ~ 2.16x** 之间（不是单向的「谁快」）：
`BenchmarkHybridSearch/*` 在 Windows 基线上偏高（`b.N` 小、含首次惰性初始化），
而 `BenchmarkSnapshotDiff/identical`、`BenchmarkSearch/bm25/5000/*` 在 Linux 上反而慢近 2 倍。
跨平台硬比时 3 倍阈值永远打不到，门禁会亮绿灯却没在拦任何东西——所以各用各的。

## 一条命令

```bash
# 与 CI 完全相同的口径（须逐字一致：-bench 模式决定基线里有哪些基准）
go test ./internal/service/ -run '^$' \
  -bench '^(BenchmarkTokenize|BenchmarkIndex|BenchmarkSearch|BenchmarkHybridSearch|BenchmarkSnapshot)' \
  -benchmem -count=3 | tee bench.txt

# 本地用本平台基线；CI 用 baseline-linux.txt
go run ./scripts/benchcheck -baseline benchmarks/baseline.txt -current bench.txt
```

`benchcheck` 会打印一张「基线 → 当前」的对比表；退出码 1 表示有指标超出容忍度，
2 表示输入读不出来（文件缺失、基准根本没跑——不会当成「没有回归」放过去）。

## 覆盖范围

| 基准 | 测什么 | 为什么值得盯 |
| :--- | :--- | :--- |
| `BenchmarkTokenizeIndex` / `BenchmarkTokenizeBigram` | 分词：gse `CutAll`（索引与查询的真实路径）与 bigram 兜底 | 索引与查询共用的最底层 kernel，改分词策略会直接影响召回与耗时 |
| `BenchmarkIndexBuild` | 冷启动全量建索引，100 / 1000 / 5000 篇三档 | 5.6s/5000 篇、722MB 分配——首次启动与大批量导入的主要成本 |
| `BenchmarkIndexSummary` | 索引摘要（Gob）落盘与重载 | 代码注释点名的「最主要的性能热点」；决定冷启动时长 |
| `BenchmarkSearch` | `bm25` = 纯内存打分 kernel；`search` = 全链路（含 walk 全库 + snippet） | 两者差值就是「检索之外」的固定开销，是优化的入口 |
| `BenchmarkHybridSearch` | BM25 + 向量（RRF 融合），`bm25-only` 与 `hybrid` 两条 arm | 语义检索的延迟上限；向量端点用确定性桩，不依赖 Ollama |
| `BenchmarkSnapshotDiff` / `BenchmarkSnapshotService` | 行级 LCS diff（纯计算）与快照服务读路径（两次 gzip 读取 + 索引解析 + 回滚） | 版本历史面板的打开速度；`truncated=1` 表示 DP 超限退化成整块差异 |

语料由固定种子（`perfSeed = 20261126`）的 `math/rand` 生成，任何机器上逐字节相同；
3 档规模（100/1000/5000）的语料互为前缀，跨档对比不受语料差异干扰。

## 门禁怎么判

| 指标 | 阈值 | 判据 |
| :--- | :--- | :--- |
| `ns/op`（耗时） | **3 倍** | 多次测量的**最小值**对比。托管 runner 抖动 ±50% 很常见，窄阈值必然误红；3 倍足以放过机器差异，又能抓住 O(n) → O(n²) 这类 10 倍以上的算法级退化 |
| `allocs/op`（分配次数） | **1.5 倍** | 给定输入下基本确定性，不随机器快慢变化，所以可以严一点 |
| `B/op`（分配字节） | **2 倍** | 同上，允许少量浮动（map/slice 扩容时机差异） |

三条附加规则：

- **跨平台只判耗时**。内存指标只在同一操作系统内可比：Windows 的系统调用包装层
  （UTF-16 路径转换等）比 Linux 多几次分配，文件类基准硬比必然误红。
  基线平台与当前平台不一致时，`benchcheck` 会打印提示并只对耗时判定；
  但只要按上表用对应平台的基线，正常路径不会走到这条降级逻辑。
- **基线里没有的基准**只报告为 `NEW`，不判失败（新增基准不必同步改阈值）。
- **当前输出里缺失的基准**给出警告但不失败——但如果是有意改名，请同步更新基线。

## 更新基线

两个文件各自的来源，别串：

```bash
# CI（Linux）基线：从 CI 产物刷新，保证与门禁同平台同核数
gh run download <run-id> -n benchmark-results        # 得到 bench.txt
cp bench.txt benchmarks/baseline-linux.txt

# 本机（Windows）基线：本地跑，产出直接落盘
go test ./internal/service/ -run '^$' \
  -bench '^(BenchmarkTokenize|BenchmarkIndex|BenchmarkSearch|BenchmarkHybridSearch|BenchmarkSnapshot)' \
  -benchmem -count=3 > benchmarks/baseline.txt
```

两个注意点：

- 生成基线时**不要同时跑构建/测试**，否则 CPU 争抢会把基线抬高，门禁等于被放宽。
- 测的是「取最小值」，所以单次抖动不会污染基线；但如果机器一直在降频（笔记本省电模式），
  基线会整体偏高——换机器刷新时留意 `cpu:` 表头行（CI runner 是 4 核，本机 6C12T，
  `-4`/`-12` 后缀就是 GOMAXPROCS，跑并行基准时也会影响结果）。

## 基线数据

以下各表是本机（Windows 11 / AMD Ryzen 5 7500F 6C12T / Go 1.27.4，`-count=3` 取最小值）。
绝对数字换台机器会变，**可比的是同一平台内基线与当前之间的比值**。

同一份代码在 CI 的 Linux runner（Xeon Platinum 8573C，4 核）上的量级，供对照——
注意 `-4` 后缀是 GOMAXPROCS=4，且慢于 1s 的基准会退化成 `b.N=1`（见下文混合检索）：

| 场景 | Windows（本机） | Linux（CI） | 备注 |
| :--- | ---: | ---: | :--- |
| 建索引 5000 篇 | 5.62 s（1.12 ms/篇） | 1.52 s（0.30 ms/篇） | 分配量完全一致（720MB / 8.49M allocs），差异全在 CPU |
| 摘要重载 5000 篇 | 205 ms | 214 ms | 两平台接近，Gob 解码不吃 CPU 特性 |
| `bm25/5000/common` | 2.1 ms | 3.83 ms | Linux 反而慢 1.9 倍 |
| `bm25/5000/rare` | 0.5 µs | 1.08 µs | 同上 |
| `hybrid/5000/common` | 1.05 s（`b.N=1`） | 56 ms（`b.N=20`） | 见下 |

### 检索内核（5000 篇，纯内存，无 walk）

| 查询类型 | ns/op | 说明 |
| :--- | ---: | :--- |
| `rare`（单篇独有编号） | 约 0.5 µs | 倒排表长度 1，最乐观 |
| `english`（单个英文术语） | 约 49 µs | 200 条命中 |
| `topic`（单个主题词） | 约 107 µs | 200 条命中（结果上限截断） |
| `multi`（两个主题词） | 约 227 µs | 多 token 求交 |
| `common`（全库共有词） | 约 2.1 ms | 候选集 ≈ 全库，最坏情况；分配 414KB/次 |

### 建索引与摘要

| 场景 | 耗时 | 分配 | 备注 |
| :--- | ---: | ---: | :--- |
| 建索引 100 篇 | 95.6 ms | 14.6 MB | 0.96 ms/篇 |
| 建索引 1000 篇 | 1.05 s | 147 MB | 1.05 ms/篇 |
| 建索引 5000 篇 | 5.62 s | 722 MB | 1.12 ms/篇，8.5M allocs |
| 摘要落盘 5000 篇 | 58 ms | 49.5 MB | 产物 5.3 MB（1063 B/篇） |
| 摘要重载 5000 篇 | 205 ms | 136 MB | 冷启动的主要部分 |

### 检索全链路（含 walk 全库 + 即时 snippet）

| 场景 | ns/op | 备注 |
| :--- | ---: | :--- |
| `search/100/common` | 约 18 ms | 100 篇也要 walk 全库 |
| `bm25/5000/rare` | 0.54 µs | 纯内核，对照用 |

### 混合检索

| 场景 | ns/op | 备注 |
| :--- | ---: | :--- |
| `hybrid/5000/*`（本机） | 约 1.03–1.08 s | **`b.N=1`**：单次调用就超过 `-benchtime` 默认的 1s，`testing.B` 无法再折算出稳态值；71.7K allocs 里含首次惰性成本 |
| `hybrid/5000/*`（CI） | 45–57 ms | `b.N≈20`，51.2K allocs = 稳态。与上行的差额是「首次调用 vs 稳态」，不是平台差异 |

结论：**「混合检索要 1 秒」是测量口径造成的假象**。想看稳态必须让 `b.N ≥ 3`，
在慢机上就显式 `-benchtime=10x`（跑 10 次而不是按时间），否则永远只得一次冷调用。

### 快照

| 场景 | ns/op | 备注 |
| :--- | ---: | :--- |
| `SnapshotDiff/replace-5pct`（2000 行改 100 行） | 6.7 ms | 15.5 MB 分配，`truncated=0` |
| `SnapshotDiff/identical` | 79 µs | 无改动时快速返回 |
| `SnapshotService/DiffSnapshots` | 8.1 ms | 含两次 gzip 读取 + 索引解析 |
| `SnapshotService/RestoreSnapshot` | 5.7 ms | 含安全备份 + 原子写回 |

## 大库压测（20000 篇）

默认跳过，手动触发：

```bash
NV_STRESS_TEST=1 go test ./internal/service/ -run TestStressIndexLargeVault -v -count=1 -timeout 25m
```

语料：20000 篇、正文 44.1 MB、平均 2313 字节/篇；落盘约 30s（临时目录，跑完自动清理）。

**这里只测全链路延迟，不测检索内核**：本机 `time.Now()` 的最小非零步进实测约
**525 µs**（20 万次连续采样里 199998 次读数为 0），亚毫秒的 `scoreBM25` 会被量化成
0 或 0.5 ms，产出「P50 = 0 µs、P95 = 500 µs」这种双峰假数据。内核延迟看
`BenchmarkSearch` 的 `bm25/*` 分项——`testing.B` 在多轮迭代内折算，不受计时粒度限制。

### 真实路径（当前版本行为：受 `maxScanFiles = 5000` 约束）

| 指标 | 数值 |
| :--- | :--- |
| 实际索引 / 磁盘上 | 5000 / 20000 篇（`scanComplete=false`） |
| 建索引 | 5.2 s（1.05 ms/篇） |
| 内存峰值 | HeapAlloc +352 MB / Sys +327 MB |
| 常驻内存 | 151 MB（31.7 KB/篇，含正文缓存，预算上限 32 MB） |
| 摘要 | 4.97 MB（1043 B/篇），落盘 45 ms |
| 冷启动 | LoadSummary 145 ms + refresh 358 ms ≈ **503 ms** |
| 查询（全链路，无 watcher） | P50 376 ms / P95 449 ms |

### 推算路径（绕过上限，20000 篇全部进索引）

| 指标 | 数值 |
| :--- | :--- |
| 建索引 | 24.1 s（1.21 ms/篇，线性外推成立） |
| 内存峰值 | HeapAlloc +978 MB / Sys +405 MB |
| 常驻内存 | 499 MB（26.2 KB/篇，不含正文缓存） |
| 摘要 | 20.27 MB（1063 B/篇），落盘 212 ms |
| 冷启动 | LoadSummary 761 ms + refresh 355 ms ≈ **1.12 s** |
| 查询（全链路，无 watcher） | P50 376 ms / P95 446 ms |

### 结论

1. **建索引与查询的复杂度都是线性的**：1.12ms/篇在 100/1000/5000/20000 四档都稳定，
   没有出现超线性退化；20000 篇的成本是 22.8s / 1GB 峰值分配 / 499MB 常驻。
2. **`maxScanFiles = 5000` 是产品级上限，不是 bug**：超出部分不进索引，但通过
   `GetIndexStats().scanComplete = false` 暴露给状态栏（PRD 的 P0-5 设计），
   用户不会「搜不到还以为没有」。是否提高上限取决于上面这组成本能否接受。
3. **全链路查询 400ms 与内核亚微秒的差距不在 BM25**：差额来自每次 `Search` 的
   「walk 全库 + 生成 snippet」。watcher 活跃时 `skipRefreshable()` 会短路 walk，
   交互态不会付这笔钱；但**启动后首批查询、或 watcher 有积压事件时**会付。
   这是后续最值得优化的点（见下）。
4. **检索内核在稀疏查询上是亚微秒、最坏情况（全库候选）2.1ms**，符合
   TODO 里「5,000 篇 <1.2ms」的量级判断（最坏情况略超）。

### 后续优化候选（按性价比）

| 候选 | 依据 |
| :--- | :--- |
| 降低建索引的分配量 | 722MB / 5000 篇 = 144KB/篇，8.5M 次分配；预分配 token 切片与复用缓冲区可直接砍掉大头 |
| 摘要加载提速 | 5000 篇 205ms / 136MB，20000 篇 944ms；Gob 是同步阻塞的，可考虑并行解码或分片加载 |
| 全链路 Search 避免重复 walk | 冷路径 P50 400ms，其中 walk 占绝大多数；可对「无 watcher 的只读查询」加短 TTL 的扫描结果缓存 |
| 混合检索 1s/次复核 | ✅ 已复核：不是真实延迟。本机 1.05s 是 `b.N=1` 的单次冷调用（含向量库惰性初始化，71.7K allocs）；CI 上 `b.N≈20` 的稳态是 56ms / 51.2K allocs。要复现稳态，在慢机上用 `-benchtime=10x` |

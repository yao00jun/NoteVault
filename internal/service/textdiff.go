package service

import "strings"

// 行级文本 diff（纯 Go，无外部依赖）。
//
// 为什么不引 go-git / go-diff：版本快照（P1-2）只需要「两份 Markdown 的行差异」，
// 引一整个 git 实现或 diff 库来换这一个函数，违背项目「零外部依赖」的立身之本。
// 笔记体量下（通常 < 5000 行）自研 LCS 足够，且能精确控制算力上限。

// DiffOpType 描述一行在 diff 中的角色
const (
	// DiffEqual 两侧相同
	DiffEqual = "equal"
	// DiffInsert 仅新版有（新增行）
	DiffInsert = "insert"
	// DiffDelete 仅旧版有（删除行）
	DiffDelete = "delete"
	// DiffGap 折叠掉的未变更区域（Count 为折叠行数），仅用于前端展示省略号
	DiffGap = "gap"
)

const (
	// diffContextLines 变更行上下保留的上下文行数（对齐 unified diff 惯例）
	diffContextLines = 3
	// diffCellLimit LCS 动态规划的格子上限。
	// 4e6 个 int32 ≈ 16MB，是内存与精度的折中；超限退化为「整块替换」，
	// 并置 Truncated=true 让前端明确告知用户，而不是悄悄给出错误的 diff。
	diffCellLimit = 4_000_000
	// diffMaxOps 返回给前端的最大操作数，防止超大文件把 IPC payload 撑爆
	diffMaxOps = 8000
)

// DiffOp 表示 diff 中的一行操作
type DiffOp struct {
	Type    string `json:"type"`
	OldLine int    `json:"oldLine"` // 1-based；insert 时为 0
	NewLine int    `json:"newLine"` // 1-based；delete 时为 0
	Text    string `json:"text"`
	Count   int    `json:"count,omitempty"` // 仅 DiffGap 使用：折叠的行数
}

// DiffResult 是一次 diff 的完整结果
type DiffResult struct {
	Ops       []DiffOp `json:"ops"`
	Added     int      `json:"added"`
	Removed   int      `json:"removed"`
	Truncated bool     `json:"truncated"` // 超出算力/体积上限，结果为粗粒度近似
}

// splitLines 按行切分，统一吃掉 \r（Windows 编辑器写出的 CRLF 不应被算成差异）
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = strings.TrimSuffix(line, "\r")
	}
	// 末尾换行会切出一个空串，去掉以免每次 diff 都多一行空白差异
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

// diffText 计算两段文本的行级差异，并折叠未变更区域
func diffText(oldText, newText string) *DiffResult {
	ops, truncated := diffLines(splitLines(oldText), splitLines(newText))

	added, removed := 0, 0
	for _, op := range ops {
		switch op.Type {
		case DiffInsert:
			added++
		case DiffDelete:
			removed++
		}
	}

	ops = collapseUnchanged(ops, diffContextLines)
	if len(ops) > diffMaxOps {
		ops = ops[:diffMaxOps]
		truncated = true
	}

	return &DiffResult{Ops: ops, Added: added, Removed: removed, Truncated: truncated}
}

// diffLines 计算行序列差异。
// 先剥离公共前后缀（真实编辑绝大多数只改中间一小段，这一步能把 DP 规模降几个数量级），
// 再对剩余中段做 LCS。
func diffLines(a, b []string) ([]DiffOp, bool) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	midA := a[prefix : len(a)-suffix]
	midB := b[prefix : len(b)-suffix]

	ops := make([]DiffOp, 0, len(a)+len(b))
	for i := 0; i < prefix; i++ {
		ops = append(ops, DiffOp{Type: DiffEqual, OldLine: i + 1, NewLine: i + 1, Text: a[i]})
	}

	truncated := false
	if len(midA)*len(midB) > diffCellLimit {
		// 退化路径：整块删除 + 整块新增。结果仍然正确（能表达"变成了什么"），
		// 只是丢掉了块内的细粒度对齐。
		truncated = true
		for i, line := range midA {
			ops = append(ops, DiffOp{Type: DiffDelete, OldLine: prefix + i + 1, Text: line})
		}
		for i, line := range midB {
			ops = append(ops, DiffOp{Type: DiffInsert, NewLine: prefix + i + 1, Text: line})
		}
	} else {
		ops = append(ops, lcsOps(midA, midB, prefix)...)
	}

	for i := 0; i < suffix; i++ {
		oldIdx := len(a) - suffix + i
		newIdx := len(b) - suffix + i
		ops = append(ops, DiffOp{Type: DiffEqual, OldLine: oldIdx + 1, NewLine: newIdx + 1, Text: a[oldIdx]})
	}

	return ops, truncated
}

// lcsOps 用 LCS 动态规划求最小编辑脚本。offset 为中段在原序列中的起始下标（用于还原真实行号）。
func lcsOps(a, b []string, offset int) []DiffOp {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]DiffOp, 0, m)
		for j := 0; j < m; j++ {
			ops = append(ops, DiffOp{Type: DiffInsert, NewLine: offset + j + 1, Text: b[j]})
		}
		return ops
	}
	if m == 0 {
		ops := make([]DiffOp, 0, n)
		for i := 0; i < n; i++ {
			ops = append(ops, DiffOp{Type: DiffDelete, OldLine: offset + i + 1, Text: a[i]})
		}
		return ops
	}

	// dp[i][j] = a[i:] 与 b[j:] 的 LCS 长度。用一维 slice 手工寻址省内存。
	stride := m + 1
	dp := make([]int32, (n+1)*stride)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i*stride+j] = dp[(i+1)*stride+j+1] + 1
			} else if dp[(i+1)*stride+j] >= dp[i*stride+j+1] {
				dp[i*stride+j] = dp[(i+1)*stride+j]
			} else {
				dp[i*stride+j] = dp[i*stride+j+1]
			}
		}
	}

	ops := make([]DiffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, DiffOp{
				Type: DiffEqual, OldLine: offset + i + 1, NewLine: offset + j + 1, Text: a[i],
			})
			i++
			j++
		case dp[(i+1)*stride+j] >= dp[i*stride+j+1]:
			ops = append(ops, DiffOp{Type: DiffDelete, OldLine: offset + i + 1, Text: a[i]})
			i++
		default:
			ops = append(ops, DiffOp{Type: DiffInsert, NewLine: offset + j + 1, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, DiffOp{Type: DiffDelete, OldLine: offset + i + 1, Text: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, DiffOp{Type: DiffInsert, NewLine: offset + j + 1, Text: b[j]})
	}
	return ops
}

// collapseUnchanged 把远离变更点的连续 equal 段折叠成一个 DiffGap，
// 让"改了 3 行的 2000 行笔记"不必把 2000 行全传到前端。
func collapseUnchanged(ops []DiffOp, context int) []DiffOp {
	if len(ops) == 0 {
		return ops
	}
	keep := make([]bool, len(ops))
	hasChange := false
	for i, op := range ops {
		if op.Type == DiffEqual {
			continue
		}
		hasChange = true
		keep[i] = true
		for d := 1; d <= context; d++ {
			if i-d >= 0 {
				keep[i-d] = true
			}
			if i+d < len(ops) {
				keep[i+d] = true
			}
		}
	}
	if !hasChange {
		// 完全相同：不返回全文，只给一个 gap 说明「N 行无差异」
		return []DiffOp{{Type: DiffGap, Count: len(ops)}}
	}

	out := make([]DiffOp, 0, len(ops))
	gap := 0
	flushGap := func() {
		if gap > 0 {
			out = append(out, DiffOp{Type: DiffGap, Count: gap})
			gap = 0
		}
	}
	for i, op := range ops {
		if keep[i] {
			flushGap()
			out = append(out, op)
			continue
		}
		gap++
	}
	flushGap()
	return out
}

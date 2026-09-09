package service

import (
	"fmt"
	"strconv"
)

// Verified streams come only from top-level objects scanned within their declared
// byte boundaries. Never discover an index or object by searching binary data.
type sourcePDFVerifiedStream struct {
	object sourcePDFObjectID
	fields map[string][]string
	data   []byte
}

func sourcePDFValueEnd(tokens []string, start int) (int, error) {
	if start >= len(tokens) {
		return 0, fmt.Errorf("PDF 字典值缺失")
	}
	if tokens[start] != "<<" && tokens[start] != "[" {
		if start+2 < len(tokens) && tokens[start+2] == "R" {
			if _, err := strconv.ParseInt(tokens[start], 10, 64); err == nil {
				if _, err := strconv.ParseInt(tokens[start+1], 10, 64); err == nil {
					return start + 3, nil
				}
			}
		}
		return start + 1, nil
	}
	stack := []string{}
	for i := start; i < len(tokens); i++ {
		switch tokens[i] {
		case "<<", "[":
			stack = append(stack, tokens[i])
			if len(stack) > 128 {
				return 0, fmt.Errorf("PDF 容器嵌套过深")
			}
		case ">>", "]":
			if len(stack) == 0 || tokens[i] == ">>" && stack[len(stack)-1] != "<<" || tokens[i] == "]" && stack[len(stack)-1] != "[" {
				return 0, fmt.Errorf("PDF 容器边界无效")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("PDF 容器未结束")
}

func sourcePDFDictionary(tokens []string) (map[string][]string, error) {
	fields := map[string][]string{}
	if len(tokens) < 2 || tokens[0] != "<<" || tokens[len(tokens)-1] != ">>" {
		return nil, fmt.Errorf("PDF 流字典无效")
	}
	for i := 1; i < len(tokens)-1; {
		key := tokens[i]
		if len(key) < 2 || key[0] != '/' || fields[key] != nil {
			return nil, fmt.Errorf("PDF 流字典键重复或无效")
		}
		end, err := sourcePDFValueEnd(tokens[:len(tokens)-1], i+1)
		if err != nil {
			return nil, err
		}
		fields[key] = tokens[i+1 : end]
		i = end
	}
	return fields, nil
}

func sourcePDFDirectInteger(value []string, limit int64) (int64, error) {
	if len(value) != 1 {
		return 0, fmt.Errorf("PDF 索引与对象流尺寸必须为直接整数")
	}
	return sourcePDFInteger(value[0], limit)
}

func sourcePDFIntegerArray(value []string) ([]int64, error) {
	if len(value) < 2 || value[0] != "[" || value[len(value)-1] != "]" || len(value) > 1002 {
		return nil, fmt.Errorf("PDF 索引数组无效")
	}
	var result []int64
	for _, token := range value[1 : len(value)-1] {
		n, err := sourcePDFInteger(token, 200000)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, nil
}

func sourcePDFPlainStream(stream sourcePDFVerifiedStream) error {
	// The bytes validated below must exactly match the dependency's decoded
	// bytes. Predictors/indirect filters need their own bounded decoder first.
	for _, key := range []string{"/DecodeParms", "/Extends", "/Prev", "/XRefStm"} {
		value := stream.fields[key]
		if len(value) > 0 && !(len(value) == 1 && value[0] == "null") {
			return fmt.Errorf("暂不支持此索引或对象流参数：%s", key)
		}
	}
	filters := stream.fields["/Filter"]
	if len(filters) > 1 && filters[0] == "[" && filters[len(filters)-1] == "]" {
		filters = filters[1 : len(filters)-1]
	}
	for _, filter := range filters {
		if filter != "/FlateDecode" && filter != "/ASCII85Decode" {
			return fmt.Errorf("索引或对象流编码不支持")
		}
	}
	return nil
}

func sourcePDFObjectMembers(stream sourcePDFVerifiedStream) ([]int64, error) {
	if err := sourcePDFPlainStream(stream); err != nil {
		return nil, err
	}
	n, err := sourcePDFDirectInteger(stream.fields["/N"], 200000)
	if err != nil {
		return nil, err
	}
	first, err := sourcePDFDirectInteger(stream.fields["/First"], int64(len(stream.data)))
	if err != nil || first == 0 {
		return nil, fmt.Errorf("PDF 对象流正文偏移无效")
	}
	ids, offsets := make([]int64, 0, n), make([]int, 0, n)
	seen := map[int64]bool{}
	position := 0
	for i := int64(0); i < n; i++ {
		token, next, err := sourcePDFToken(stream.data[:first], position)
		if err != nil {
			return nil, err
		}
		id, err := sourcePDFInteger(token, 200000)
		if err != nil || id == 0 || seen[id] {
			return nil, fmt.Errorf("PDF 对象流编号重复或无效")
		}
		token, position, err = sourcePDFToken(stream.data[:first], next)
		if err != nil {
			return nil, err
		}
		offset, err := sourcePDFInteger(token, int64(len(stream.data))-first)
		if err != nil || i > 0 && int(offset)+int(first) <= offsets[len(offsets)-1] {
			return nil, fmt.Errorf("PDF 对象流偏移无效")
		}
		seen[id] = true
		ids, offsets = append(ids, id), append(offsets, int(first+offset))
	}
	if sourcePDFSkipSpace(stream.data[:first], position) != int(first) {
		return nil, fmt.Errorf("PDF 对象流头部长度无效")
	}
	for i, offset := range offsets {
		end := len(stream.data)
		if i+1 < len(offsets) {
			end = offsets[i+1]
		}
		part := stream.data[offset:end]
		var tokens []string
		for at := 0; sourcePDFSkipSpace(part, at) < len(part); {
			token, next, err := sourcePDFToken(part, at)
			if err != nil {
				return nil, err
			}
			if token == "obj" || token == "endobj" || token == "stream" || token == "endstream" {
				return nil, fmt.Errorf("PDF 对象流含非法嵌套对象")
			}
			tokens, at = append(tokens, token), next
		}
		valueEnd, err := sourcePDFValueEnd(tokens, 0)
		if err != nil || valueEnd != len(tokens) {
			return nil, fmt.Errorf("PDF 对象流成员边界无效")
		}
	}
	return ids, nil
}

func sourcePDFCompressedReferences(stream sourcePDFVerifiedStream, streams map[int]sourcePDFVerifiedStream, objects map[int]sourcePDFObjectID) ([]sourcePDFXrefEntry, error) {
	if kind := stream.fields["/Type"]; len(kind) != 1 || kind[0] != "/XRef" {
		return nil, fmt.Errorf("PDF 索引必须引用 XRef 流")
	}
	if err := sourcePDFPlainStream(stream); err != nil {
		return nil, err
	}
	size, err := sourcePDFDirectInteger(stream.fields["/Size"], 200000)
	if err != nil || size == 0 {
		return nil, fmt.Errorf("PDF 索引尺寸无效")
	}
	widths, err := sourcePDFIntegerArray(stream.fields["/W"])
	if err != nil || len(widths) != 3 {
		return nil, fmt.Errorf("PDF 索引宽度无效")
	}
	width := 0
	for _, w := range widths {
		if w > 8 {
			return nil, fmt.Errorf("PDF 索引宽度超出上限")
		}
		width += int(w)
	}
	if width == 0 {
		return nil, fmt.Errorf("PDF 索引宽度为零")
	}
	index := []int64{0, size}
	if stream.fields["/Index"] != nil {
		index, err = sourcePDFIntegerArray(stream.fields["/Index"])
		if err != nil || len(index)%2 != 0 {
			return nil, fmt.Errorf("PDF 索引范围无效")
		}
	}
	type compressedEntry struct{ kind, a, b int64 }
	entries := map[int64]compressedEntry{}
	position := 0
	for i := 0; i < len(index); i += 2 {
		start, count := index[i], index[i+1]
		if start+count > size || count > int64((len(stream.data)-position)/width) {
			return nil, fmt.Errorf("PDF 索引范围超出边界")
		}
		for id := start; id < start+count; id++ {
			if _, exists := entries[id]; exists {
				return nil, fmt.Errorf("PDF 索引范围重叠")
			}
			values := [3]int64{}
			for j, w := range widths {
				for b := int64(0); b < w; b++ {
					values[j] = values[j]*256 + int64(stream.data[position])
					position++
					if values[j] > sourceMaxFileBytes {
						return nil, fmt.Errorf("PDF 索引字段超出上限")
					}
				}
			}
			if widths[0] == 0 {
				values[0] = 1
			}
			if values[0] > 2 {
				return nil, fmt.Errorf("PDF 索引条目类型无效")
			}
			entries[id] = compressedEntry{values[0], values[1], values[2]}
		}
	}
	if position != len(stream.data) {
		return nil, fmt.Errorf("PDF 索引流长度不匹配")
	}
	var rows []sourcePDFXrefEntry
	members := map[int64][]int64{}
	for id, entry := range entries {
		switch entry.kind {
		case 1:
			object := sourcePDFObjectID{number: id, generation: entry.b}
			if entry.b > 65535 || objects[int(entry.a)] != object {
				return nil, fmt.Errorf("PDF 索引未指向对应的顶层对象")
			}
			rows = append(rows, sourcePDFXrefEntry{object: object, offset: int(entry.a)})
		case 2:
			container, exists := entries[entry.a]
			if !exists || container.kind != 1 || container.b != 0 {
				return nil, fmt.Errorf("PDF 压缩对象必须引用直接对象流")
			}
			owner, exists := streams[int(container.a)]
			if !exists || owner.object.number != entry.a || len(owner.fields["/Type"]) != 1 || owner.fields["/Type"][0] != "/ObjStm" {
				return nil, fmt.Errorf("PDF 压缩对象所属流无效")
			}
			ids, exists := members[entry.a]
			if !exists {
				ids, err = sourcePDFObjectMembers(owner)
				if err != nil {
					return nil, err
				}
				members[entry.a] = ids
			}
			if entry.b >= int64(len(ids)) || ids[entry.b] != id {
				return nil, fmt.Errorf("PDF 压缩对象编号或偏移不匹配")
			}
		}
	}
	return rows, nil
}

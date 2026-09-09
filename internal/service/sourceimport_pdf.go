package service

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

// PDF uses its own token separators: names may contain #xx escapes, comments
// are whitespace, and ">> stream" is legal. Tokenizing the container before
// invoking the PDF reader avoids regex gaps around decompression/allocation
// limits. String bodies and binary streams are never interpreted as commands.
func sourcePDFSpace(value byte) bool {
	return value == 0 || value == 9 || value == 10 || value == 12 || value == 13 || value == 32
}

func sourcePDFSkipSpace(data []byte, offset int) int {
	for offset < len(data) {
		if sourcePDFSpace(data[offset]) {
			offset++
		} else if data[offset] == '%' {
			for offset < len(data) && data[offset] != '\n' && data[offset] != '\r' {
				offset++
			}
		} else {
			break
		}
	}
	return offset
}

func sourcePDFToken(data []byte, offset int) (string, int, error) {
	offset = sourcePDFSkipSpace(data, offset)
	if offset >= len(data) {
		return "", offset, io.EOF
	}
	start := offset
	value := data[offset]
	offset++
	if value == '(' {
		depth := 1
		for offset < len(data) && depth > 0 {
			switch data[offset] {
			case '\\':
				offset++
			case '(':
				depth++
			case ')':
				depth--
			}
			offset++
			if depth > 128 {
				return "", offset, fmt.Errorf("PDF 字符串嵌套过深")
			}
		}
		if depth != 0 {
			return "", offset, fmt.Errorf("PDF 字符串未结束")
		}
		return "()", offset, nil
	}
	if value == '<' {
		if offset < len(data) && data[offset] == '<' {
			return "<<", offset + 1, nil
		}
		for offset < len(data) && data[offset] != '>' {
			offset++
		}
		if offset == len(data) {
			return "", offset, fmt.Errorf("PDF 十六进制字符串未结束")
		}
		return "<>", offset + 1, nil
	}
	if value == '>' && offset < len(data) && data[offset] == '>' {
		return ">>", offset + 1, nil
	}
	if strings.ContainsRune("[]{}>)", rune(value)) {
		return string(value), offset, nil
	}
	for offset < len(data) && !sourcePDFSpace(data[offset]) && !strings.ContainsRune("()<>[]{}/%", rune(data[offset])) {
		offset++
	}
	if offset-start > 65536 {
		return "", offset, fmt.Errorf("PDF 标记过长")
	}
	token := string(data[start:offset])
	if value == '/' {
		var decoded strings.Builder
		for i := 0; i < len(token); i++ {
			if token[i] == '#' && i+2 < len(token) {
				n, err := strconv.ParseUint(token[i+1:i+3], 16, 8)
				if err == nil {
					decoded.WriteByte(byte(n))
					i += 2
					continue
				}
			}
			decoded.WriteByte(token[i])
		}
		token = decoded.String()
	}
	return token, offset, nil
}

func sourcePDFInteger(token string, limit int64) (int64, error) {
	n, err := strconv.ParseInt(token, 10, 64)
	if err != nil || n < 0 || n > limit {
		return 0, fmt.Errorf("PDF 数值声明超出解析上限")
	}
	return n, nil
}

func sourcePDFCheckTokens(tokens []string) error {
	// /W also names CID font glyph widths; /Count may be negative in closed
	// outlines. Only XRef dictionaries use these as allocation dimensions.
	xrefScopes := map[int]bool{}
	stack := []int{}
	for i, token := range tokens {
		switch token {
		case "<<":
			stack = append(stack, i)
		case ">>":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case "/Type":
			if len(stack) > 0 && i+1 < len(tokens) && tokens[i+1] == "/XRef" {
				xrefScopes[stack[len(stack)-1]] = true
			}
		}
	}
	stack = nil
	for i, token := range tokens {
		if token == "<<" {
			stack = append(stack, i)
		}
		if token == ">>" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
		inXref := len(stack) > 0 && xrefScopes[stack[len(stack)-1]]
		limit := int64(-1)
		switch token {
		case "/N":
			limit = 200000
		case "/Size":
			if inXref {
				limit = 200000
			}
		case "/Columns":
			limit = 10000
		case "/Length":
			limit = sourceMaxFileBytes
		case "obj":
			if i < 2 {
				return fmt.Errorf("PDF 对象声明无效")
			}
			for _, value := range tokens[i-2 : i] {
				if _, err := sourcePDFInteger(value, 200000); err != nil {
					return err
				}
			}
		}
		if limit >= 0 {
			if i+1 >= len(tokens) {
				return fmt.Errorf("PDF 数值声明缺失")
			}
			if _, err := sourcePDFInteger(tokens[i+1], limit); err != nil {
				return err
			}
			if token == "/Columns" && i+3 < len(tokens) && tokens[i+3] == "R" {
				return fmt.Errorf("不支持间接 PDF 预测器尺寸")
			}
		}
		if inXref && (token == "/W" || token == "/Index") {
			if i+1 >= len(tokens) || tokens[i+1] != "[" {
				return fmt.Errorf("PDF 索引数组无效")
			}
			values := []int64{}
			for j := i + 2; j < len(tokens) && tokens[j] != "]"; j++ {
				n, err := sourcePDFInteger(tokens[j], 200000)
				if err != nil {
					return err
				}
				values = append(values, n)
				if len(values) > 1000 {
					return fmt.Errorf("PDF 索引数组过大")
				}
			}
			if token == "/W" {
				if len(values) != 3 {
					return fmt.Errorf("PDF 索引宽度无效")
				}
				for _, width := range values {
					if width > 8 {
						return fmt.Errorf("PDF 索引宽度超出上限")
					}
				}
			} else {
				if len(values)%2 != 0 {
					return fmt.Errorf("PDF 索引范围无效")
				}
				for j := 0; j < len(values); j += 2 {
					if values[j]+values[j+1] > 200000 {
						return fmt.Errorf("PDF 索引范围过大")
					}
				}
			}
		}
	}
	return nil
}

var errSourcePDFReferences = errors.New("PDF 索引格式无法安全验证")

type sourcePDFObjectID struct {
	number, generation int64
}

type sourcePDFXrefEntry struct {
	object sourcePDFObjectID
	offset int
}

func sourcePDFReadXref(data []byte, offset int) (int, []sourcePDFXrefEntry, error) {
	entries := int64(0)
	rows := []sourcePDFXrefEntry{}
	for {
		start := offset
		token, next, err := sourcePDFToken(data, offset)
		if err != nil {
			return offset, nil, fmt.Errorf("PDF 索引未结束")
		}
		if token == "trailer" {
			return start, rows, nil
		}
		first, err := sourcePDFInteger(token, 200000)
		if err != nil {
			return offset, nil, err
		}
		token, offset, err = sourcePDFToken(data, next)
		if err != nil {
			return offset, nil, err
		}
		count, err := sourcePDFInteger(token, 200000)
		if err != nil || first+count > 200000 {
			return offset, nil, fmt.Errorf("PDF 索引范围过大")
		}
		entries += count
		if entries > 200000 {
			return offset, nil, fmt.Errorf("PDF 索引条目过多")
		}
		for i := int64(0); i < count; i++ {
			token, afterOffset, err := sourcePDFToken(data, offset)
			position, positionErr := sourcePDFInteger(token, int64(len(data)))
			if err != nil || positionErr != nil {
				return offset, nil, fmt.Errorf("PDF 索引偏移无效")
			}
			token, afterGeneration, err := sourcePDFToken(data, afterOffset)
			generation, generationErr := sourcePDFInteger(token, 65535)
			if err != nil || generationErr != nil {
				return offset, nil, fmt.Errorf("PDF 索引对象代数无效")
			}
			token, offset, err = sourcePDFToken(data, afterGeneration)
			if err != nil || token != "n" && token != "f" {
				return offset, nil, fmt.Errorf("PDF 索引条目无效")
			}
			if token == "n" {
				rows = append(rows, sourcePDFXrefEntry{object: sourcePDFObjectID{number: first + i, generation: generation}, offset: int(position)})
			}
		}
	}
}

func sourcePDFCheckReferences(data []byte, marker, table int, rows []sourcePDFXrefEntry, objects map[int]sourcePDFObjectID) error {
	// Match the dependency's last-100-byte, whole-line startxref selection.
	// A visible earlier marker cannot authorize a later marker hidden in a
	// literal or stream, nor can lexical scanning authorize a hidden object.
	tailStart := len(data) - 100
	tail := bytes.TrimRight(data[tailStart:], "\r\n\t ")
	if !bytes.HasSuffix(tail, []byte("%%EOF")) {
		return fmt.Errorf("%w：缺少文件结束标记", errSourcePDFReferences)
	}
	selected := -1
	for limit := len(tail); limit > 0; {
		i := bytes.LastIndex(tail[:limit], []byte("startxref"))
		if i <= 0 || i+len("startxref") >= len(tail) {
			break
		}
		if (tail[i-1] == '\n' || tail[i-1] == '\r') && (tail[i+len("startxref")] == '\n' || tail[i+len("startxref")] == '\r') {
			selected = tailStart + i
			break
		}
		limit = i
	}
	if selected < 0 || selected != marker || table < 0 {
		return fmt.Errorf("%w：最终索引必须位于已验证的文件顶层", errSourcePDFReferences)
	}
	token, _, err := sourcePDFToken(data, selected+len("startxref"))
	offset, valueErr := sourcePDFInteger(token, int64(len(data)-1))
	if err != nil || valueErr != nil || int(offset) != table {
		return fmt.Errorf("%w：最终偏移未指向普通索引表", errSourcePDFReferences)
	}
	for _, row := range rows {
		object, found := objects[row.offset]
		if !found || object != row.object {
			return fmt.Errorf("%w：对象偏移未指向对应的顶层对象", errSourcePDFReferences)
		}
	}
	return nil
}

func sourcePDFStreamHeader(tokens []string) (int64, []string, error) {
	length := int64(-1)
	filters := []string{}
	filterSeen := false
	depth := 0
	for i, token := range tokens {
		switch token {
		case "<<", "[":
			depth++
			continue
		case ">>", "]":
			depth--
			continue
		}
		// The reader consumes the outer stream dictionary. Nested dictionaries
		// can also contain /Length or /Filter but cannot change that boundary.
		if depth != 1 {
			continue
		}
		if token == "/Length" {
			if i+1 >= len(tokens) {
				return 0, nil, fmt.Errorf("PDF 流长度缺失")
			}
			n, err := sourcePDFInteger(tokens[i+1], sourceMaxFileBytes)
			if err != nil {
				return 0, nil, err
			}
			if i+3 < len(tokens) && tokens[i+3] == "R" {
				length = -1
			} else {
				length = n
			}
		}
		if token != "/Filter" {
			continue
		}
		if filterSeen || i+1 >= len(tokens) {
			return 0, nil, fmt.Errorf("PDF 流过滤声明有歧义")
		}
		filterSeen = true
		if tokens[i+1] == "[" {
			for j := i + 2; j < len(tokens) && tokens[j] != "]"; j++ {
				if !strings.HasPrefix(tokens[j], "/") {
					return 0, nil, fmt.Errorf("PDF 过滤链无效")
				}
				filters = append(filters, tokens[j])
			}
		} else if strings.HasPrefix(tokens[i+1], "/") {
			filters = append(filters, tokens[i+1])
		} else {
			return 0, nil, fmt.Errorf("不支持间接 PDF 过滤声明")
		}
		if len(filters) > 4 {
			return 0, nil, fmt.Errorf("PDF 过滤链过长")
		}
	}
	return length, filters, nil
}

func sourcePDFDecodeStream(ctx context.Context, data []byte, filters []string) ([]byte, error) {
	decoded := data
	for _, filter := range filters {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var reader io.Reader
		var closer io.Closer
		switch filter {
		case "/FlateDecode":
			zr, err := zlib.NewReader(bytes.NewReader(decoded))
			if err != nil {
				return nil, fmt.Errorf("PDF 压缩流损坏")
			}
			reader, closer = zr, zr
		case "/ASCII85Decode":
			encoded := bytes.TrimSpace(decoded)
			encoded = bytes.TrimPrefix(encoded, []byte("<~"))
			encoded = bytes.TrimSuffix(encoded, []byte("~>"))
			reader = ascii85.NewDecoder(bytes.NewReader(encoded))
		default:
			// Image codecs are never expanded by the text reader; unsupported
			// content codecs fail closed inside the PDF library.
			continue
		}
		content, err := io.ReadAll(io.LimitReader(reader, sourceMaxFileBytes+1))
		if closer != nil {
			_ = closer.Close()
		}
		if err != nil || len(content) > sourceMaxFileBytes {
			return nil, fmt.Errorf("PDF 解码流损坏或超过 20 MiB 上限")
		}
		decoded = content
	}
	return decoded, nil
}

func sourcePDFCheckObjectStream(ctx context.Context, decoded []byte) error {
	tokens := []string{}
	for offset := 0; offset < len(decoded); {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, next, err := sourcePDFToken(decoded, offset)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if token == "stream" {
			return fmt.Errorf("对象流中不能嵌套 PDF 流")
		}
		tokens = append(tokens, token)
		if len(tokens) > 500000 {
			return fmt.Errorf("PDF 对象流标记过多")
		}
		offset = next
	}
	return sourcePDFCheckTokens(tokens)
}

func sourcePDFPreflight(ctx context.Context, data []byte) error {
	if len(data) < 100 || len(data) > sourceMaxFileBytes || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return fmt.Errorf("PDF 文件无效或过大")
	}
	tokens := []string{}
	objects := map[int]sourcePDFObjectID{}
	streamsByOffset := map[int]sourcePDFVerifiedStream{}
	objectOffset, compressedIndex := -1, -1
	objectHadStream := false
	rows := []sourcePDFXrefEntry{}
	containers := []string{}
	previousOffsets := [2]int{}
	inObject, inTrailer, expectTrailer := false, false, false
	table, marker := -1, -1
	var header []string
	depth, dictStart, offset, streams := 0, 0, 0, 0
	var total int64
	for offset < len(data) {
		if err := ctx.Err(); err != nil {
			return err
		}
		tokenOffset := sourcePDFSkipSpace(data, offset)
		token, next, err := sourcePDFToken(data, tokenOffset)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		offset = next
		priorOffsets := previousOffsets
		previousOffsets = [2]int{previousOffsets[1], tokenOffset}
		if token == "xref" {
			if inObject || len(containers) != 0 || table >= 0 {
				return fmt.Errorf("%w：仅支持单个普通索引表", errSourcePDFReferences)
			}
			table, expectTrailer = tokenOffset, true
			offset, rows, err = sourcePDFReadXref(data, offset)
			if err != nil {
				return fmt.Errorf("%w：%v", errSourcePDFReferences, err)
			}
			continue
		}
		tokens = append(tokens, token)
		if len(tokens) > 500000 {
			return fmt.Errorf("PDF 标记数量超出上限")
		}
		switch token {
		case "obj":
			if inObject || len(containers) != 0 || len(tokens) < 3 {
				return fmt.Errorf("%w：对象声明不在文件顶层", errSourcePDFReferences)
			}
			number, numberErr := sourcePDFInteger(tokens[len(tokens)-3], 200000)
			generation, generationErr := sourcePDFInteger(tokens[len(tokens)-2], 65535)
			if numberErr != nil || generationErr != nil {
				return fmt.Errorf("%w：对象编号无效", errSourcePDFReferences)
			}
			objects[priorOffsets[0]] = sourcePDFObjectID{number: number, generation: generation}
			objectOffset = priorOffsets[0]
			objectHadStream = false
			inObject, header = true, nil
		case "endobj":
			if !inObject || len(containers) != 0 {
				return fmt.Errorf("%w：对象边界无效", errSourcePDFReferences)
			}
			inObject, header = false, nil
		case "trailer":
			if !expectTrailer || inObject || len(containers) != 0 {
				return fmt.Errorf("%w：索引尾字典边界无效", errSourcePDFReferences)
			}
			inTrailer, expectTrailer = true, false
		case "/Prev", "/XRefStm":
			if inTrailer {
				return fmt.Errorf("%w：暂不提取增量或混合索引", errSourcePDFReferences)
			}
		case "startxref":
			if inObject || len(containers) != 0 {
				return fmt.Errorf("%w：索引指针不在文件顶层", errSourcePDFReferences)
			}
			marker, inTrailer = tokenOffset, false
		case "[":
			containers = append(containers, token)
			if len(containers) > 128 {
				return fmt.Errorf("PDF 容器嵌套过深")
			}
		case "]":
			if len(containers) == 0 || containers[len(containers)-1] != "[" {
				return fmt.Errorf("PDF 数组边界无效")
			}
			containers = containers[:len(containers)-1]
		case "<<":
			containers = append(containers, token)
			if depth == 0 {
				dictStart = len(tokens) - 1
			}
			depth++
			if len(containers) > 128 {
				return fmt.Errorf("PDF 字典嵌套过深")
			}
		case ">>":
			if len(containers) == 0 || containers[len(containers)-1] != "<<" {
				return fmt.Errorf("PDF 字典边界无效")
			}
			containers = containers[:len(containers)-1]
			depth--
			if depth < 0 {
				return fmt.Errorf("PDF 字典无效")
			}
			if depth == 0 {
				header = tokens[dictStart:]
			}
		case "stream":
			if objectHadStream {
				return fmt.Errorf("%w：一个对象不能包含多个流", errSourcePDFReferences)
			}
			objectHadStream = true
			streams++
			if streams > 20000 || len(header) == 0 || !inObject || len(containers) != 0 || len(tokens) < 2 || tokens[len(tokens)-2] != ">>" {
				return fmt.Errorf("PDF 流数量或声明无效")
			}
			for offset < len(data) && (data[offset] == ' ' || data[offset] == '\t') {
				offset++
			}
			if offset >= len(data) || (data[offset] != '\r' && data[offset] != '\n') {
				return fmt.Errorf("PDF 流缺少换行")
			}
			if data[offset] == '\r' {
				offset++
			}
			if offset < len(data) && data[offset] == '\n' {
				offset++
			}
			length, filters, err := sourcePDFStreamHeader(header)
			if err != nil {
				return err
			}
			if length < 0 {
				// Searching for endstream inside arbitrary binary data cannot
				// establish the actual object boundaries used by the reader.
				return fmt.Errorf("%w：暂不提取间接流长度", errSourcePDFReferences)
			}
			end := 0
			if length > int64(len(data)-offset) {
				return fmt.Errorf("PDF 流长度无效")
			}
			end = offset + int(length)
			endToken, endOffset, err := sourcePDFToken(data, end)
			if err != nil || endToken != "endstream" {
				return fmt.Errorf("PDF 流长度与边界不一致")
			}
			decoded, err := sourcePDFDecodeStream(ctx, data[offset:end], filters)
			if err != nil {
				return err
			}
			for i, token := range header {
				if token == "/Type" && i+1 < len(header) && header[i+1] == "/ObjStm" {
					if err := sourcePDFCheckObjectStream(ctx, decoded); err != nil {
						return err
					}
				}
			}
			fields, err := sourcePDFDictionary(header)
			if err != nil {
				return err
			}
			if kind := fields["/Type"]; len(kind) == 1 && (kind[0] == "/XRef" || kind[0] == "/ObjStm") {
				streamsByOffset[objectOffset] = sourcePDFVerifiedStream{object: objects[objectOffset], fields: fields, data: decoded}
				if kind[0] == "/XRef" {
					if compressedIndex >= 0 || table >= 0 {
						return fmt.Errorf("%w：暂不支持增量或混合索引", errSourcePDFReferences)
					}
					compressedIndex = objectOffset
				}
			}
			total += int64(len(decoded))
			if total > sourceMaxTotalBytes {
				return fmt.Errorf("PDF 解码总量超过 100 MiB 上限")
			}
			offset = endOffset
			header = nil
		}
	}
	if inObject || len(containers) != 0 || expectTrailer {
		return fmt.Errorf("%w：容器未结束", errSourcePDFReferences)
	}
	if compressedIndex >= 0 {
		if table >= 0 {
			return fmt.Errorf("%w：暂不支持混合索引", errSourcePDFReferences)
		}
		var err error
		rows, err = sourcePDFCompressedReferences(streamsByOffset[compressedIndex], streamsByOffset, objects)
		if err != nil {
			return fmt.Errorf("%w：%v", errSourcePDFReferences, err)
		}
		table = compressedIndex
	}
	if err := sourcePDFCheckReferences(data, marker, table, rows, objects); err != nil {
		return err
	}
	return sourcePDFCheckTokens(tokens)
}

type sourcePDFReader struct {
	ctx    context.Context
	reader *bytes.Reader
	reads  int
	bytes  int64
}

func (r *sourcePDFReader) ReadAt(data []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	r.reads++
	r.bytes += int64(len(data))
	if r.reads > 200000 || r.bytes > 640<<20 {
		return 0, fmt.Errorf("PDF 解析操作超出上限")
	}
	return r.reader.ReadAt(data, offset)
}

func sourceExtractPDF(ctx context.Context, data []byte) (text string, err error) {
	defer func() {
		if recover() != nil {
			text = ""
			err = fmt.Errorf("PDF 文本解析失败")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := sourcePDFPreflight(ctx, data); err != nil {
		return "", err
	}
	reader, err := pdf.NewReader(&sourcePDFReader{ctx: ctx, reader: bytes.NewReader(data)}, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("PDF 文件加密、损坏或不支持")
	}
	pages := []pdf.Page{}
	nodes := 0
	var walk func(pdf.Value, int) error
	walk = func(node pdf.Value, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		nodes++
		if depth > 32 || nodes > 2000 || len(pages) >= 500 {
			return fmt.Errorf("PDF 页树超出上限")
		}
		if node.Key("Type").Name() == "Page" {
			pages = append(pages, pdf.Page{V: node})
			return nil
		}
		children := node.Key("Kids")
		if children.Len() > 500 {
			return fmt.Errorf("PDF 页数超出上限")
		}
		for i := 0; i < children.Len(); i++ {
			if err := walk(children.Index(i), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(reader.Trailer().Key("Root").Key("Pages"), 0); err != nil {
		return "", err
	}
	return sourcePDFMarkdown(ctx, pages)
}

package service

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const sourcePDFExtractionVersion = 1

// Text objects in a PDF are drawing instructions, not paragraphs. In particular,
// a font change in Chinese/Latin text often starts a new object on the same line.
type sourcePDFLine struct {
	raw, markdown                         string
	x, y, right, size, height             float64
	bold, mono                            float64
	page, heading                         int
	code                                  bool
	gutter                                int
	codeX                                 float64
	codeRaw                               string
	leadingSpace, trailingSpace, fallback bool
}

type sourcePDFSpan struct {
	text           string
	font           string
	x, width, size float64
	bold, mono     bool
	precise        bool
}

var (
	sourcePDFList         = regexp.MustCompile(`^(?:[-*+]\s+|[•●◦▪‣]\s*|[0-9]{1,3}[.)、]\s+)`)
	sourcePDFQuestion     = regexp.MustCompile(`^[0-9]{1,3}[.、)）]\s*.+[?？]$`)
	sourcePDFHeadingStart = regexp.MustCompile(`^(?:[0-9]{1,3}[.、)）]|[一二三四五六七八九十百]+[、.]|第.+[章节部分])`)
	sourcePDFPageNumber   = regexp.MustCompile(`^(?:[-–—]\s*)?[0-9]{1,4}(?:\s*[-–—])?$`)
	sourcePDFGutter       = regexp.MustCompile(`^[0-9]{1,4}$`)
	sourcePDFEscape       = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "|", "\\|")
)

func sourcePDFFontStyle(font string) (bold, mono bool) {
	name := strings.ToLower(font)
	return strings.Contains(name, "bold") || strings.Contains(name, "black") || strings.Contains(name, "heavy"),
		strings.Contains(name, "mono") || strings.Contains(name, "courier") || strings.Contains(name, "consolas") || strings.Contains(name, "menlo")
}

func sourcePDFGlyphWidth(glyph pdf.Text) float64 {
	if glyph.W > 0 {
		return glyph.W
	}
	// The reader does not expose CID widths. Some exporters position every glyph;
	// others position a whole run. Estimate only the missing advance, retaining
	// stable source order for glyphs that share an X coordinate.
	var width float64
	_, mono := sourcePDFFontStyle(glyph.Font)
	for _, char := range glyph.S {
		switch {
		case unicode.Is(unicode.Han, char) || unicode.Is(unicode.Hiragana, char) || unicode.Is(unicode.Katakana, char) || char >= 0xff00 && char <= 0xffef:
			width += glyph.FontSize
		case mono:
			width += glyph.FontSize * .6
		case unicode.IsSpace(char):
			width += glyph.FontSize * .25
		default:
			width += glyph.FontSize * .5
		}
	}
	return width
}

func sourcePDFWord(char rune) bool {
	return char < 0x2e80 && (unicode.IsLetter(char) || unicode.IsDigit(char))
}

func sourcePDFJoinSpace(left, right string) string {
	last, _ := utf8.DecodeLastRuneInString(left)
	first, _ := utf8.DecodeRuneInString(right)
	if (sourcePDFWord(last) || strings.ContainsRune("\"')]}", last)) && sourcePDFWord(first) {
		return " "
	}
	return ""
}

func sourcePDFHasCJK(text string) bool {
	for _, char := range text {
		if unicode.Is(unicode.Han, char) || unicode.Is(unicode.Hiragana, char) || unicode.Is(unicode.Katakana, char) || unicode.Is(unicode.Hangul, char) {
			return true
		}
	}
	return false
}

func sourcePDFWrapSpace(previous, next sourcePDFLine, cjk bool) string {
	if previous.trailingSpace || next.leadingSpace {
		return " "
	}
	// CJK typesetting wraps inside Latin identifiers and URLs too. A physical
	// line break is not evidence of a word boundary in such a paragraph.
	if cjk || sourcePDFHasCJK(next.raw) {
		return ""
	}
	return sourcePDFJoinSpace(previous.raw, next.raw)
}

func sourcePDFJoinInline(left, right, separator string) string {
	if strings.HasSuffix(left, "**") && strings.HasPrefix(right, "**") {
		return strings.TrimSuffix(left, "**") + separator + strings.TrimPrefix(right, "**")
	}
	end := len(left) - len(strings.TrimRight(left, "`"))
	start := len(right) - len(strings.TrimLeft(right, "`"))
	if end > 0 && end == start && (len(left) == end || left[len(left)-end-1] != '\\') {
		return left[:len(left)-end] + separator + right[start:]
	}
	return left + separator + right
}

func sourcePDFStyled(text string, bold, mono bool) string {
	content := strings.TrimSpace(text)
	if content == "" {
		return text
	}
	leading := text[:len(text)-len(strings.TrimLeftFunc(text, unicode.IsSpace))]
	trailing := text[len(strings.TrimRightFunc(text, unicode.IsSpace)):]
	if mono {
		fence := "`"
		for strings.Contains(content, fence) {
			fence += "`"
		}
		if strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") {
			content = " " + content + " "
		}
		return leading + fence + content + fence + trailing
	}
	content = sourcePDFEscape.Replace(content)
	if bold {
		content = "**" + content + "**"
	}
	return leading + content + trailing
}

func sourcePDFMakeLine(glyphs []pdf.Text, page int, height float64) sourcePDFLine {
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].X < glyphs[j].X })
	spans := []sourcePDFSpan{}
	sizes := map[float64]int{}
	line := sourcePDFLine{page: page, height: height, x: math.Inf(1), y: glyphs[0].Y}
	count, boldCount, monoCount := 0, 0, 0
	var joined strings.Builder
	for _, glyph := range glyphs {
		bold, mono := sourcePDFFontStyle(glyph.Font)
		n := utf8.RuneCountInString(strings.TrimSpace(glyph.S))
		count += n
		if bold {
			boldCount += n
		}
		if mono {
			monoCount += n
		}
		sizes[math.Round(glyph.FontSize*10)/10] += n
		if n > 0 {
			line.x = math.Min(line.x, glyph.X)
		}
		width := sourcePDFGlyphWidth(glyph)
		if len(spans) > 0 {
			last := &spans[len(spans)-1]
			if math.Abs(last.x-glyph.X) < .01 && glyph.W == 0 && last.font == glyph.Font && last.bold == bold && last.mono == mono {
				if joined.Len() == 0 {
					joined.WriteString(last.text)
				}
				joined.WriteString(glyph.S)
				last.width += width
				continue
			}
		}
		if joined.Len() > 0 {
			spans[len(spans)-1].text = joined.String()
			joined.Reset()
		}
		spans = append(spans, sourcePDFSpan{text: glyph.S, font: glyph.Font, x: glyph.X, width: width, size: glyph.FontSize, bold: bold, mono: mono, precise: glyph.W > 0})
	}
	if joined.Len() > 0 {
		spans[len(spans)-1].text = joined.String()
	}
	weight := 0
	for size, n := range sizes {
		if n > weight || n == weight && size > line.size {
			line.size, weight = size, n
		}
	}
	if count > 0 {
		line.bold, line.mono = float64(boldCount)/float64(count), float64(monoCount)/float64(count)
	}
	var raw, markdown, run strings.Builder
	styleBold, styleMono := false, false
	gutterEnd := 0.0
	flush := func() { markdown.WriteString(sourcePDFStyled(run.String(), styleBold, styleMono)); run.Reset() }
	for i, span := range spans {
		space := ""
		if i > 0 {
			previous := spans[i-1]
			if prefix := strings.TrimSpace(raw.String()); sourcePDFGutter.MatchString(prefix) && strings.TrimSpace(span.text) != "" && span.x-gutterEnd >= span.size {
				line.gutter, _ = strconv.Atoi(prefix)
				line.codeX = span.x
			}
			// Missing CID advances are estimates for layout, not evidence that
			// whitespace existed. Keep explicit spaces but do not invent them.
			if previous.precise && span.x-(previous.x+previous.width) > math.Min(span.size, previous.size)*.35 {
				space = sourcePDFJoinSpace(previous.text, span.text)
			}
		}
		if span.bold != styleBold || span.mono != styleMono {
			flush()
			styleBold, styleMono = span.bold, span.mono
		}
		raw.WriteString(space + span.text)
		if sourcePDFGutter.MatchString(strings.TrimSpace(raw.String())) && strings.TrimSpace(span.text) != "" {
			gutterEnd = span.x + span.width
		}
		run.WriteString(space + span.text)
		if strings.TrimSpace(span.text) != "" {
			line.right = math.Max(line.right, span.x+span.width)
		}
	}
	flush()
	line.raw, line.markdown = strings.TrimSpace(raw.String()), strings.TrimSpace(markdown.String())
	first := true
	for i, span := range spans {
		if strings.TrimSpace(span.text) == "" {
			continue
		}
		if first {
			line.leadingSpace = len(span.text) != len(strings.TrimLeftFunc(span.text, unicode.IsSpace))
			if i > 0 && spans[i-1].font == span.font && strings.TrimSpace(spans[i-1].text) == "" && span.x-(spans[i-1].x+spans[i-1].width) <= span.size*.6 {
				line.leadingSpace = true
			}
			first = false
		}
		line.trailingSpace = len(span.text) != len(strings.TrimRightFunc(span.text, unicode.IsSpace))
		if i+1 < len(spans) && spans[i+1].font == span.font && strings.TrimSpace(spans[i+1].text) == "" && spans[i+1].x-(span.x+span.width) <= span.size*.6 {
			// Ignore the unrelated space some exporters draw at the right margin.
			line.trailingSpace = true
		}
	}
	if line.gutter > 0 {
		line.codeRaw = strings.TrimSpace(strings.TrimPrefix(line.raw, strconv.Itoa(line.gutter)))
	}
	return line
}

func sourcePDFReadLines(ctx context.Context, page pdf.Page, number int) ([]sourcePDFLine, error) {
	if err := sourcePDFCheckLayout(ctx, page, sourcePDFLayoutLimit); err != nil {
		return nil, err
	}
	glyphs := page.Content().Text
	if len(glyphs) > sourcePDFLayoutLimit {
		return nil, fmt.Errorf("PDF 单页文字数量超出上限")
	}
	filtered := glyphs[:0]
	for _, glyph := range glyphs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if math.IsNaN(glyph.X) || math.IsInf(glyph.X, 0) || math.IsNaN(glyph.Y) || math.IsInf(glyph.Y, 0) || math.IsNaN(glyph.FontSize) || math.IsInf(glyph.FontSize, 0) {
			return nil, fmt.Errorf("PDF 文字坐标无效")
		}
		if glyph.FontSize < .1 {
			if strings.TrimSpace(glyph.S) != "" {
				// Content exposes only the horizontal font-size component. A
				// vertical text matrix reports zero, so preserve this page's text
				// in source order instead of silently dropping visible content.
				text, err := page.GetPlainText(nil)
				if err != nil {
					return nil, err
				}
				var blocks []string
				for _, raw := range strings.Split(text, "\n") {
					if raw = strings.TrimSpace(raw); raw != "" {
						block := sourcePDFEscape.Replace(raw)
						if strings.HasPrefix(block, "#") {
							block = "\\" + block
						}
						blocks = append(blocks, block)
					}
				}
				return []sourcePDFLine{{raw: strings.TrimSpace(text), markdown: strings.Join(blocks, "\n\n"), page: number, fallback: true}}, nil
			}
			continue
		}
		glyph.S = strings.Map(func(char rune) rune {
			if char == '\n' || char == '\r' || char == '\u00ad' {
				return -1
			}
			if unicode.IsSpace(char) {
				return ' '
			}
			if unicode.IsControl(char) {
				return -1
			}
			return char
		}, glyph.S)
		if glyph.S != "" {
			filtered = append(filtered, glyph)
		}
	}
	glyphs = filtered
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].Y > glyphs[j].Y })
	height := 792.0
	for node, depth := page.V, 0; !node.IsNull() && depth < 32; node, depth = node.Key("Parent"), depth+1 {
		if box := node.Key("MediaBox"); box.Len() == 4 {
			height = box.Index(3).Float64()
			break
		}
	}
	var lines []sourcePDFLine
	for start := 0; start < len(glyphs); {
		end := start + 1
		for end < len(glyphs) && glyphs[start].Y-glyphs[end].Y <= math.Max(1, math.Min(glyphs[start].FontSize, glyphs[end].FontSize)*.25) {
			end++
		}
		line := sourcePDFMakeLine(glyphs[start:end], number, height)
		if line.raw != "" {
			lines = append(lines, line)
		}
		start = end
	}
	return lines, nil
}

func sourcePDFPrepareLines(lines []sourcePDFLine, pages int) ([]sourcePDFLine, float64) {
	sizes := map[float64]int{}
	bodySizes := map[float64]int{}
	for _, line := range lines {
		if !line.fallback && line.mono < .5 {
			sizes[line.size] += utf8.RuneCountInString(line.raw)
			if line.bold < .8 {
				bodySizes[line.size] += utf8.RuneCountInString(line.raw)
			}
		}
	}
	if len(bodySizes) == 0 {
		bodySizes = sizes
	}
	bodySize, weight := 12.0, 0
	for size, n := range bodySizes {
		if n > weight || n == weight && size < bodySize {
			bodySize, weight = size, n
		}
	}
	var headingSizes []float64
	for size := range sizes {
		if size > bodySize*1.2 {
			headingSizes = append(headingSizes, size)
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(headingSizes)))
	margins := map[string]map[int]bool{}
	for _, line := range lines {
		if !line.fallback && line.size <= bodySize*1.1 && (line.y < 36 || line.y > line.height-36) {
			if margins[line.raw] == nil {
				margins[line.raw] = map[int]bool{}
			}
			margins[line.raw][line.page] = true
		}
	}
	filtered := lines[:0]
	for _, line := range lines {
		if line.fallback {
			filtered = append(filtered, line)
			continue
		}
		inMargin := line.size <= bodySize*1.1 && (line.y < 36 || line.y > line.height-36)
		if inMargin && (len(margins[line.raw]) >= max(2, (pages+1)/2) || line.y < 36 && sourcePDFPageNumber.MatchString(line.raw)) {
			continue
		}
		if line.mono < .8 && utf8.RuneCountInString(line.raw) < 200 {
			for i, size := range headingSizes {
				if math.Abs(line.size-size) < .2 {
					line.heading = min(6, i+2)
					break
				}
			}
			if line.heading == 0 && sourcePDFQuestion.MatchString(line.raw) {
				line.heading = min(6, len(headingSizes)+2)
			}
		}
		filtered = append(filtered, line)
	}
	// A wrapped inline example can fill a whole line. Promote monospace groups
	// only when they are separate from adjacent prose containing inline code.
	candidate := func(line sourcePDFLine) bool { return line.mono >= .95 || line.gutter > 0 && line.mono > 0 }
	adjacent := func(a, b sourcePDFLine) bool { return a.page != b.page || a.y-b.y <= bodySize*1.8 }
	for i := 0; i < len(filtered); {
		if !candidate(filtered[i]) {
			i++
			continue
		}
		end := i + 1
		for end < len(filtered) && candidate(filtered[end]) && adjacent(filtered[end-1], filtered[end]) {
			end++
		}
		inlineBefore := i > 0 && filtered[i-1].mono > 0 && filtered[i-1].mono < .95 && adjacent(filtered[i-1], filtered[i])
		inlineAfter := end < len(filtered) && filtered[end].mono > 0 && filtered[end].mono < .95 && adjacent(filtered[end-1], filtered[end])
		for j := i; j < end; j++ {
			filtered[j].code = filtered[j].gutter > 0 || (!inlineBefore && !inlineAfter)
		}
		i = end
	}
	return filtered, bodySize
}

func sourcePDFParagraphEnd(text string) bool {
	return strings.ContainsAny(stringLastRune(text), ".。!！?？:：;；")
}

func stringLastRune(text string) string {
	last, _ := utf8.DecodeLastRuneInString(strings.TrimRight(text, " \"'”’）)】]"))
	return string(last)
}

func sourcePDFNewParagraph(previous, current sourcePDFLine, body, right float64) bool {
	if previous.page != current.page {
		return sourcePDFParagraphEnd(previous.raw)
	}
	if previous.y-current.y > body*2 || math.Abs(previous.size-current.size) > body*.25 {
		return true
	}
	return sourcePDFParagraphEnd(previous.raw) && (previous.right < right-body*2 || current.x-previous.x > body)
}

func sourcePDFMarkdown(ctx context.Context, pages []pdf.Page) (string, error) {
	var lines []sourcePDFLine
	total := 0
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		pageLines, err := sourcePDFReadLines(ctx, page, i)
		if err != nil {
			return "", err
		}
		for _, line := range pageLines {
			total += len(line.markdown)
		}
		if total > sourceMaxFileBytes {
			return "", fmt.Errorf("PDF 正文超出 20 MiB 上限")
		}
		lines = append(lines, pageLines...)
	}
	lines, body := sourcePDFPrepareLines(lines, len(pages))
	rights := map[int]float64{}
	for _, line := range lines {
		if line.heading == 0 && !line.code && !line.fallback {
			rights[line.page] = math.Max(rights[line.page], line.right)
		}
	}
	var out strings.Builder
	previousList := false
	for i := 0; i < len(lines); {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := lines[i]
		list := sourcePDFList.MatchString(line.raw) && line.heading == 0 && !line.code && !line.fallback
		if out.Len() > 0 {
			if list && previousList {
				out.WriteByte('\n')
			} else {
				out.WriteString("\n\n")
			}
		}
		previousList = list
		if line.fallback {
			out.WriteString(line.markdown)
			i++
		} else if line.heading > 0 {
			title := line.raw
			cjk := sourcePDFHasCJK(line.raw)
			previous := line
			i++
			for i < len(lines) {
				next := lines[i]
				if next.page != previous.page || next.heading != line.heading || sourcePDFHeadingStart.MatchString(next.raw) || sourcePDFParagraphEnd(previous.raw) || previous.y-next.y > previous.size*1.7 || previous.right < rights[previous.page]-previous.size*1.5 {
					break
				}
				title += sourcePDFWrapSpace(previous, next, cjk) + next.raw
				cjk = cjk || sourcePDFHasCJK(next.raw)
				previous = next
				i++
			}
			out.WriteString(strings.Repeat("#", line.heading) + " " + sourcePDFEscape.Replace(title))
		} else if line.code {
			end, left := i+1, line.x
			for end < len(lines) && lines[end].code && (lines[end-1].page != lines[end].page || lines[end-1].y-lines[end].y < body*2.5) {
				left = math.Min(left, lines[end].x)
				end++
			}
			var code strings.Builder
			gutter := end-i >= 2 && line.gutter > 0
			for j := i; j < end && gutter; j++ {
				gutter = lines[j].gutter == line.gutter+j-i && math.Abs(lines[j].x-line.x) < body*.5
			}
			if gutter {
				left = lines[i].codeX
				for _, row := range lines[i:end] {
					left = math.Min(left, row.codeX)
				}
			}
			for _, row := range lines[i:end] {
				x, text := row.x, row.raw
				if gutter {
					x, text = row.codeX, row.codeRaw
				}
				indent := min(80, max(0, int(math.Round((x-left)/(row.size*.6)))))
				code.WriteString(strings.Repeat(" ", indent) + text + "\n")
			}
			fence := "```"
			for strings.Contains(code.String(), fence) {
				fence += "`"
			}
			out.WriteString(fence + "\n" + code.String() + fence)
			i = end
		} else {
			paragraph := ""
			if list {
				prefix := sourcePDFList.FindString(line.raw)
				marker := strings.TrimSpace(prefix)
				if len(marker) > 0 && marker[0] >= '0' && marker[0] <= '9' {
					marker = strings.TrimRight(marker, ".)、") + "."
				} else {
					marker = "-"
				}
				paragraph = marker + " " + sourcePDFEscape.Replace(strings.TrimPrefix(line.raw, prefix))
			} else {
				markdown := line.markdown
				if strings.HasPrefix(markdown, "#") {
					markdown = "\\" + markdown
				}
				paragraph = markdown
			}
			i++
			previous := line
			cjk := sourcePDFHasCJK(line.raw)
			for i < len(lines) && lines[i].heading == 0 && !lines[i].code && !lines[i].fallback && !sourcePDFList.MatchString(lines[i].raw) {
				next := lines[i]
				if sourcePDFNewParagraph(previous, next, body, rights[previous.page]) {
					break
				}
				if list && next.page == previous.page && next.x < line.x+body*.5 {
					break
				}
				paragraph = sourcePDFJoinInline(paragraph, next.markdown, sourcePDFWrapSpace(previous, next, cjk))
				cjk = cjk || sourcePDFHasCJK(next.raw)
				previous = next
				i++
			}
			out.WriteString(paragraph)
		}
		if out.Len() > sourceMaxFileBytes {
			return "", fmt.Errorf("PDF 正文超出 20 MiB 上限")
		}
	}
	return strings.TrimSpace(out.String()), nil
}

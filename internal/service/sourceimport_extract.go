package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/net/html"
)

func sourceUTF8Prefix(value string, limit int) string {
	if limit < 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func sourceDecodeText(data []byte) (string, error) {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		if len(data)%2 != 0 {
			return "", fmt.Errorf("UTF-16 文本长度无效")
		}
		var order binary.ByteOrder = binary.BigEndian
		if data[0] == 0xff {
			order = binary.LittleEndian
		}
		units := make([]uint16, (len(data)-2)/2)
		for i := range units {
			units[i] = order.Uint16(data[2+i*2:])
		}
		return string(utf16.Decode(units)), nil
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("不是可读取的 UTF-8/UTF-16 文本")
	}
	return strings.TrimPrefix(string(data), "\ufeff"), nil
}

func sourceAsset(name string) bool {
	if sourceImage(name) {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".mp3", ".wav", ".ogg", ".mp4", ".webm":
		return true
	}
	return false
}

func sourceImage(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".avif", ".svg":
		return true
	}
	return false
}

func sourceCodeLanguage(extension string) string {
	switch extension {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".jsx":
		return "jsx"
	case ".vue":
		return "vue"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".sh", ".bash", ".zsh", ".ps1":
		return "text"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".sql":
		return "sql"
	case ".css", ".scss":
		return "css"
	case ".ini", ".properties", ".cfg":
		return "text"
	}
	return ""
}

func sourceExtractInput(ctx context.Context, input sourceImportInput, budget *sourceImportBudget, depth int) ([]sourceImportOutput, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if depth == 0 {
		if err := budget.add(int64(len(input.data))); err != nil {
			return nil, nil, err
		}
	}
	name, err := sourceRelativePath(input.name)
	if err != nil {
		return nil, nil, err
	}
	if sourceIgnoredPath(name) {
		return nil, []string{"已忽略凭据、缓存或生成文件：" + name}, nil
	}
	output := sourceImportOutput{name: name, origin: input.origin, source: input.name, sourceHash: sourceChecksum(input.data), data: input.data}
	if isMarkdownFile(name) {
		output.extractionMode = "markdown-preserved"
		return []sourceImportOutput{output}, nil, nil
	}
	if sourceAsset(name) {
		output.extractionMode = "attachment"
		if sourceImage(name) {
			output.warning = "已保留原始图片附件：" + name + "（未识别图片中的文字；如需正文请先进行 OCR）"
		}
		return []sourceImportOutput{output}, nil, nil
	}
	extension := strings.ToLower(path.Ext(name))
	output.name = strings.TrimSuffix(name, path.Ext(name)) + ".md"
	title := strings.TrimSuffix(path.Base(name), path.Ext(name))
	var content string
	var assets []sourceImportOutput
	switch extension {
	case ".zip":
		if depth >= 2 {
			return nil, nil, fmt.Errorf("压缩包嵌套超过 2 层")
		}
		entries, err := sourceReadArchive(ctx, input.data, budget)
		if err != nil {
			return nil, nil, err
		}
		outputs, warnings := []sourceImportOutput{}, []string{}
		prefix := strings.TrimSuffix(name, path.Ext(name))
		for _, entry := range entries {
			entry.name = path.Join(prefix, entry.name)
			entry.origin = input.origin + "!" + input.name
			extracted, notices, err := sourceExtractInput(ctx, entry, budget, depth+1)
			if err != nil {
				return nil, nil, err
			}
			outputs = append(outputs, extracted...)
			warnings = append(warnings, notices...)
		}
		return outputs, warnings, nil
	case ".docx", ".epub":
		output.extractionMode = strings.TrimPrefix(extension, ".")
		entries, err := sourceReadArchive(ctx, input.data, budget)
		if err != nil {
			return nil, nil, err
		}
		if extension == ".docx" {
			content, assets, err = sourceExtractDOCX(ctx, input, entries)
		} else {
			content, assets, err = sourceExtractEPUB(ctx, input, entries)
		}
		if err != nil {
			return nil, []string{fmt.Sprintf("无法提取 %s：%s", name, err)}, nil
		}
	case ".pdf":
		output.extractionMode = "pdf-text"
		content, err = sourceExtractPDF(ctx, input.data)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			output.name, output.extractionMode = name, "attachment"
			if errors.Is(err, errSourcePDFReferences) {
				output.warning = "已保留原始 PDF 附件：" + name + "（索引或对象引用无法安全验证，未提取正文）"
			} else {
				output.warning = "已保留原始 PDF 附件：" + name + "（无法提取正文：文件损坏、加密、格式不支持或超出安全提取上限；扫描件需要 OCR）"
			}
			return []sourceImportOutput{output}, nil, nil
		}
		if strings.TrimSpace(content) == "" {
			output.name, output.extractionMode = name, "attachment"
			output.warning = "已保留原始 PDF 附件：" + name + "（未提取到可读正文；扫描件需要 OCR）"
			return []sourceImportOutput{output}, nil, nil
		}
	case ".html", ".htm", ".xhtml":
		output.extractionMode = "html"
		var base *url.URL
		baseURL := input.baseURL
		if baseURL == "" {
			baseURL = input.origin
		}
		if strings.HasPrefix(baseURL, "https://") || strings.HasPrefix(baseURL, "http://") {
			base, _ = url.Parse(baseURL)
		}
		content, err = sourceHTMLMarkdown(ctx, input.data, func(raw string) string { return sourceHTMLLink(raw, base) })
		if err != nil {
			return nil, []string{"无法提取 HTML 正文：" + name}, nil
		}
	case ".txt", ".rst", ".csv", ".tsv", ".log":
		output.extractionMode = "text"
		content, err = sourceDecodeText(input.data)
		if err != nil {
			return nil, []string{"文本编码无法读取：" + name}, nil
		}
	default:
		output.extractionMode = "source-code"
		language := sourceCodeLanguage(extension)
		if language == "" && (strings.EqualFold(path.Base(name), "README") || strings.EqualFold(path.Base(name), "LICENSE")) {
			language = "text"
		}
		if language == "" {
			return nil, []string{"暂不支持此格式，未生成虚构正文：" + name}, nil
		}
		text, err := sourceDecodeText(input.data)
		if err != nil {
			return nil, []string{"源代码或配置不是可读取文本：" + name}, nil
		}
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		content = fence + language + "\n" + text + "\n" + fence
	}
	if strings.TrimSpace(content) == "" {
		return nil, []string{"未提取到可读正文：" + name}, nil
	}
	output.data = []byte("# " + title + "\n\n" + strings.TrimSpace(content) + "\n")
	if len(output.data) > sourceMaxFileBytes {
		return nil, nil, fmt.Errorf("提取正文超过 20 MiB 上限")
	}
	return append([]sourceImportOutput{output}, assets...), nil, nil
}

func sourceHTMLLink(raw string, base *url.URL) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || strings.ContainsAny(raw, "\x00\r\n") {
		return ""
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	return strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(u.String())
}

func sourceHTMLMarkdown(ctx context.Context, data []byte, link func(string) string) (string, error) {
	decoded, err := sourceDecodeText(data)
	if err != nil {
		return "", err
	}
	tokenizer := html.NewTokenizer(strings.NewReader(decoded))
	tokenizer.SetMaxBuf(sourceMaxFileBytes)
	var out strings.Builder
	inPre, tokens := false, 0
	ignored := []string{}
	links := []string{}
	ignoreTag := func(tag string) bool {
		switch tag {
		case "head", "script", "style", "noscript", "nav", "header", "footer", "aside", "form", "template", "svg", "canvas":
			return true
		}
		return false
	}
	headTag := func(tag string) bool {
		switch tag {
		case "html", "head", "base", "basefont", "bgsound", "link", "meta", "title", "noscript", "noframes", "style", "script", "template":
			return true
		}
		return false
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		typeOfToken := tokenizer.Next()
		if typeOfToken == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			return "", fmt.Errorf("HTML 解析失败")
		}
		tokens++
		if tokens > 200000 || out.Len() > sourceMaxFileBytes {
			return "", fmt.Errorf("HTML 超过提取上限")
		}
		token := tokenizer.Token()
		if len(ignored) > 0 {
			// Only ignored containers affect this stack. HTML permits omitted
			// </li> and </p>, so counting every descendant never finds </nav>.
			// A body-content start also implicitly closes an unclosed <head>.
			if len(ignored) == 1 && ignored[0] == "head" && typeOfToken == html.StartTagToken && !headTag(token.Data) {
				ignored = nil
			} else {
				if typeOfToken == html.StartTagToken && ignoreTag(token.Data) {
					ignored = append(ignored, token.Data)
				} else if typeOfToken == html.EndTagToken {
					for i := len(ignored) - 1; i >= 0; i-- {
						if ignored[i] == token.Data {
							ignored = ignored[:i]
							break
						}
					}
				}
				continue
			}
		}
		if typeOfToken == html.StartTagToken && ignoreTag(token.Data) {
			ignored = append(ignored, token.Data)
			continue
		}
		if typeOfToken == html.TextToken {
			if inPre {
				out.WriteString(token.Data)
			} else if text := strings.Join(strings.Fields(token.Data), " "); text != "" {
				out.WriteString(text + " ")
			}
			continue
		}
		start := typeOfToken == html.StartTagToken || typeOfToken == html.SelfClosingTagToken
		end := typeOfToken == html.EndTagToken
		if !start && !end {
			continue
		}
		switch token.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			if start {
				out.WriteString("\n\n" + strings.Repeat("#", int(token.Data[1]-'0')) + " ")
			} else {
				out.WriteString("\n\n")
			}
		case "p", "div", "section", "article", "main", "blockquote", "table", "tr", "ul", "ol":
			out.WriteString("\n\n")
		case "li":
			if start {
				out.WriteString("\n- ")
			} else {
				out.WriteByte('\n')
			}
		case "br":
			out.WriteByte('\n')
		case "hr":
			out.WriteString("\n\n---\n\n")
		case "td", "th":
			if end {
				out.WriteString(" | ")
			}
		case "pre":
			inPre = start
			if start {
				out.WriteString("\n\n~~~text\n")
			} else {
				out.WriteString("\n~~~\n\n")
			}
		case "a":
			if start {
				href := ""
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						href = link(attr.Val)
					}
				}
				links = append(links, href)
				if href != "" {
					out.WriteByte('[')
				}
			} else if len(links) > 0 {
				href := links[len(links)-1]
				links = links[:len(links)-1]
				if href != "" {
					out.WriteString("](" + href + ") ")
				}
			}
		case "img":
			if start {
				src, alt := "", "图片"
				for _, attr := range token.Attr {
					if attr.Key == "src" {
						src = link(attr.Val)
					}
					if attr.Key == "alt" {
						alt = strings.ReplaceAll(strings.ReplaceAll(attr.Val, "]", "\\]"), "\n", " ")
					}
				}
				if src != "" {
					out.WriteString("![" + alt + "](" + src + ") ")
				}
			}
		}
	}
	lines := strings.Split(out.String(), "\n")
	var cleaned strings.Builder
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		cleaned.WriteString(line + "\n")
	}
	return strings.TrimSpace(cleaned.String()), nil
}

func sourceXMLDecoder(data []byte) (*xml.Decoder, error) {
	text, err := sourceDecodeText(data)
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(strings.NewReader(text))
	// encoding/xml does not resolve DTDs or external entities. Only source text
	// is consumed; DOCX macros and EPUB scripts are never executed.
	return decoder, nil
}

func sourceExtractDOCX(ctx context.Context, input sourceImportInput, entries []sourceImportInput) (string, []sourceImportOutput, error) {
	var document []byte
	assets := []sourceImportOutput{}
	assetPrefix := strings.TrimSuffix(input.name, path.Ext(input.name)) + ".assets"
	for _, entry := range entries {
		if entry.name == "word/document.xml" {
			document = entry.data
		}
		if strings.HasPrefix(entry.name, "word/media/") && sourceAsset(entry.name) {
			assets = append(assets, sourceImportOutput{name: path.Join(assetPrefix, path.Base(entry.name)), origin: input.origin, source: input.name + "!" + entry.name, extractionMode: "attachment", data: entry.data})
		}
	}
	if document == nil {
		return "", nil, fmt.Errorf("缺少 DOCX 正文文档")
	}
	decoder, err := sourceXMLDecoder(document)
	if err != nil {
		return "", nil, err
	}
	var out, paragraph strings.Builder
	style, inText, inParagraph, numbered, depth, count := "", false, false, false, 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("DOCX 正文 XML 损坏")
		}
		count++
		if count > 1000000 || depth > 128 || out.Len()+paragraph.Len() > sourceMaxFileBytes {
			return "", nil, fmt.Errorf("DOCX 正文超过提取上限")
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			switch token.Name.Local {
			case "p":
				paragraph.Reset()
				style = ""
				numbered = false
				inParagraph = true
			case "t":
				inText = true
			case "pStyle":
				for _, attr := range token.Attr {
					if attr.Name.Local == "val" {
						style = strings.ToLower(attr.Value)
					}
				}
			case "numPr":
				numbered = true
			case "tab":
				if inParagraph {
					paragraph.WriteByte('\t')
				}
			case "br", "cr":
				if inParagraph {
					paragraph.WriteByte('\n')
				}
			}
		case xml.EndElement:
			depth--
			switch token.Name.Local {
			case "t":
				inText = false
			case "p":
				prefix := ""
				if len(style) == 8 && strings.HasPrefix(style, "heading") && style[7] >= '1' && style[7] <= '6' {
					prefix = strings.Repeat("#", int(style[7]-'0')) + " "
				} else if numbered {
					prefix = "- "
				}
				out.WriteString(prefix + paragraph.String() + "\n\n")
				inParagraph = false
			}
		case xml.CharData:
			if inText {
				paragraph.Write(token)
			}
		}
	}
	if len(assets) > 0 {
		out.WriteString("\n## 附件\n\n")
		for _, asset := range assets {
			out.WriteString("![" + path.Base(asset.name) + "](" + path.Base(assetPrefix) + "/" + path.Base(asset.name) + ")\n\n")
		}
	}
	return strings.TrimSpace(out.String()), assets, nil
}

func sourcePackagePath(base, href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "\\") {
		return "", fmt.Errorf("电子书含非法资源路径")
	}
	return sourceRelativePath(path.Join(path.Dir(base), u.Path))
}

func sourceExtractEPUB(ctx context.Context, input sourceImportInput, entries []sourceImportInput) (string, []sourceImportOutput, error) {
	files := map[string][]byte{}
	for _, entry := range entries {
		files[entry.name] = entry.data
	}
	if _, encrypted := files["META-INF/encryption.xml"]; encrypted {
		return "", nil, fmt.Errorf("加密或 DRM 电子书不支持提取")
	}
	var container struct {
		Roots []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(files["META-INF/container.xml"], &container); err != nil || len(container.Roots) == 0 {
		return "", nil, fmt.Errorf("EPUB 容器索引损坏")
	}
	opf, err := sourceRelativePath(container.Roots[0].FullPath)
	if err != nil {
		return "", nil, err
	}
	var book struct {
		Title string `xml:"metadata>title"`
		Items []struct {
			ID        string `xml:"id,attr"`
			Href      string `xml:"href,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"manifest>item"`
		Spine []struct {
			ID string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	if err := xml.Unmarshal(files[opf], &book); err != nil || len(book.Spine) == 0 || len(book.Spine) > sourceMaxFiles {
		return "", nil, fmt.Errorf("EPUB 缺少有效阅读顺序")
	}
	items := map[string]string{}
	assets := []sourceImportOutput{}
	assetPrefix := strings.TrimSuffix(input.name, path.Ext(input.name)) + ".assets"
	for _, item := range book.Items {
		relative, err := sourcePackagePath(opf, item.Href)
		if err != nil {
			return "", nil, err
		}
		items[item.ID] = relative
		if sourceAsset(relative) {
			if body, ok := files[relative]; ok {
				assets = append(assets, sourceImportOutput{name: path.Join(assetPrefix, relative), origin: input.origin, source: input.name + "!" + relative, extractionMode: "attachment", data: body})
			}
		}
	}
	var out strings.Builder
	if book.Title != "" {
		out.WriteString("# " + book.Title + "\n\n")
	}
	for _, ref := range book.Spine {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		relative, ok := items[ref.ID]
		body, present := files[relative]
		if !ok || !present {
			return "", nil, fmt.Errorf("EPUB 阅读顺序引用了缺失章节")
		}
		markdown, err := sourceHTMLMarkdown(ctx, body, func(raw string) string {
			u, err := url.Parse(raw)
			if err != nil {
				return ""
			}
			if u.IsAbs() {
				return sourceHTMLLink(raw, nil)
			}
			resource, err := sourcePackagePath(relative, raw)
			if err != nil {
				return ""
			}
			if sourceAsset(resource) {
				return sourceHTMLLink(path.Join(path.Base(assetPrefix), resource), nil)
			}
			if strings.HasPrefix(raw, "#") {
				return raw
			}
			return ""
		})
		if err != nil {
			return "", nil, err
		}
		out.WriteString(markdown + "\n\n")
		if out.Len() > sourceMaxFileBytes {
			return "", nil, fmt.Errorf("EPUB 正文超过 20 MiB 上限")
		}
	}
	return strings.TrimSpace(out.String()), assets, nil
}

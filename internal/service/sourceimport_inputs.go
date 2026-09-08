package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func sourceIgnoredPath(relative string) bool {
	for _, component := range strings.Split(strings.ToLower(strings.ReplaceAll(relative, "\\", "/")), "/") {
		if strings.HasPrefix(component, ".") {
			return true
		}
		switch component {
		case "node_modules", "vendor", "dist", "build", "out", "target", "coverage", "__pycache__", "cache", "caches", "tmp", "temp", "venv", "bin", "obj", "generated":
			return true
		}
		stem := strings.TrimSuffix(component, path.Ext(component))
		if stem == "credentials" || stem == "credential" || stem == "secrets" || stem == "secret" || stem == "token" || stem == "tokens" || stem == "apikey" || stem == "api-key" ||
			strings.HasPrefix(stem, "service-account") || strings.HasPrefix(stem, "service_account") || strings.HasPrefix(component, "id_rsa") || strings.HasPrefix(component, "id_ed25519") || strings.HasPrefix(component, "id_ecdsa") || strings.HasPrefix(component, "id_dsa") {
			return true
		}
		switch path.Ext(component) {
		case ".pem", ".key", ".p12", ".pfx", ".keystore", ".jks":
			return true
		}
	}
	return false
}

func sourceScanFolder(directory string) ([]sourceImportInput, []string, error) {
	inputs, warnings := []sourceImportInput{}, []string{}
	var total int64
	visited := 0
	err := filepath.WalkDir(directory, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("源目录含无法读取的条目")
		}
		if full == directory {
			return nil
		}
		visited++
		if visited > 10000 {
			return fmt.Errorf("目录条目过多，请选择更小的资料目录")
		}
		relative, err := filepath.Rel(directory, full)
		if err != nil {
			return fmt.Errorf("无法解析源文件路径")
		}
		relative, err = sourceRelativePath(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		if sourceIgnoredPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("源目录不能包含符号链接：%s", relative)
		}
		if strings.Count(relative, "/") > 30 {
			return fmt.Errorf("源目录嵌套超过 30 层")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("源目录含非普通文件：%s", relative)
		}
		total += info.Size()
		if info.Size() > sourceMaxFileBytes || total > sourceMaxTotalBytes || len(inputs) >= sourceMaxFiles {
			return fmt.Errorf("源资料超过 20 MiB 单文件、100 MiB 总量或 500 文件上限")
		}
		inputs = append(inputs, sourceImportInput{name: relative, local: full, origin: "folder:" + filepath.ToSlash(directory)})
		return nil
	})
	return inputs, warnings, err
}

func sourceReadLocalFile(ctx context.Context, directory, full string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := sourceDirectoryPath(directory); err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(directory, full)
	if err != nil {
		return nil, fmt.Errorf("无法解析源文件")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("无法打开源目录")
	}
	defer root.Close()
	data, exists, err := sourceReadRoot(root, filepath.ToSlash(relative), sourceMaxFileBytes)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("源文件在导入前已被移除")
	}
	return data, ctx.Err()
}

type sourceImportBudget struct {
	files int
	bytes int64
}

func (b *sourceImportBudget) add(size int64) error {
	b.files++
	b.bytes += size
	if size < 0 || size > sourceMaxFileBytes || b.bytes > sourceMaxTotalBytes || b.files > sourceMaxFiles {
		return fmt.Errorf("资料或解压内容超过 20 MiB 单文件、100 MiB 总量或 500 文件上限")
	}
	return nil
}

// Go's ZIP reader walks directory records until their signature ends; its final
// count comparison is modulo uint16. Bound the actual walk, including each
// variable-length record, before asking it to allocate a File for every entry.
func sourceArchivePreflight(data []byte) error {
	if len(data) > sourceMaxFileBytes {
		return fmt.Errorf("压缩文件超过 20 MiB 上限")
	}
	start := len(data) - (1 << 16) - 22
	if start < 0 {
		start = 0
	}
	end := bytes.LastIndex(data[start:], []byte{'P', 'K', 5, 6})
	if end < 0 || start+end+22 > len(data) {
		return fmt.Errorf("无效的 ZIP、DOCX 或 EPUB 文件")
	}
	directoryEnd := start + end
	header := data[directoryEnd:]
	entries := binary.LittleEndian.Uint16(header[10:12])
	if entries == 0xffff || int(entries) > sourceMaxFiles || binary.LittleEndian.Uint16(header[8:10]) != entries {
		return fmt.Errorf("压缩包超过 500 条目上限或目录计数不一致；不支持 ZIP64 大包")
	}
	if binary.LittleEndian.Uint16(header[4:6]) != 0 || binary.LittleEndian.Uint16(header[6:8]) != 0 || int(binary.LittleEndian.Uint16(header[20:22])) != len(header)-22 {
		return fmt.Errorf("不支持分卷或目录边界不明确的压缩包")
	}
	directorySize := uint64(binary.LittleEndian.Uint32(header[12:16]))
	directoryOffset := uint64(binary.LittleEndian.Uint32(header[16:20]))
	if directorySize == 0xffffffff || directoryOffset == 0xffffffff || directoryOffset+directorySize != uint64(directoryEnd) {
		return fmt.Errorf("压缩包目录边界无效；不支持 ZIP64 或前置可执行数据")
	}
	count := 0
	for offset := int(directoryOffset); offset < directoryEnd; {
		count++
		if count > sourceMaxFiles {
			return fmt.Errorf("压缩包实际目录超过 500 条目上限")
		}
		if directoryEnd-offset < 46 || !bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			return fmt.Errorf("压缩包中央目录损坏")
		}
		record := data[offset : offset+46]
		length := 46 + int(binary.LittleEndian.Uint16(record[28:30])) + int(binary.LittleEndian.Uint16(record[30:32])) + int(binary.LittleEndian.Uint16(record[32:34]))
		if length > directoryEnd-offset || binary.LittleEndian.Uint16(record[34:36]) != 0 || binary.LittleEndian.Uint32(record[20:24]) == 0xffffffff || binary.LittleEndian.Uint32(record[24:28]) == 0xffffffff || uint64(binary.LittleEndian.Uint32(record[42:46])) >= directoryOffset {
			return fmt.Errorf("压缩包目录记录无效；不支持分卷或 ZIP64")
		}
		offset += length
	}
	if count != int(entries) {
		return fmt.Errorf("压缩包声明与实际条目数量不一致")
	}
	return nil
}

// Both declared and actual decompressed sizes are bounded independently.
func sourceReadArchive(ctx context.Context, data []byte, budget *sourceImportBudget) ([]sourceImportInput, error) {
	if err := sourceArchivePreflight(data); err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) > sourceMaxFiles {
		return nil, fmt.Errorf("无效或条目过多的压缩包")
	}
	seen := map[string]bool{}
	var declared int64
	for _, file := range reader.File {
		name := strings.TrimSuffix(strings.ReplaceAll(file.Name, "\\", "/"), "/")
		if _, err := sourceRelativePath(name); err != nil {
			return nil, fmt.Errorf("压缩包包含非法路径")
		}
		if file.Mode()&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return nil, fmt.Errorf("压缩包不能包含符号链接或特殊文件")
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("压缩包含大小写重复路径")
		}
		seen[strings.ToLower(name)] = true
		if file.UncompressedSize64 > sourceMaxFileBytes {
			return nil, fmt.Errorf("压缩包单个条目超过 20 MiB 上限")
		}
		declared += int64(file.UncompressedSize64)
		if declared > sourceMaxTotalBytes {
			return nil, fmt.Errorf("压缩包解压总量超过 100 MiB 上限")
		}
	}
	inputs := []sourceImportInput{}
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if sourceIgnoredPath(name) {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("无法读取压缩包条目")
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, sourceMaxFileBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("压缩包条目损坏")
		}
		if err := budget.add(int64(len(content))); err != nil {
			return nil, err
		}
		inputs = append(inputs, sourceImportInput{name: name, data: content})
	}
	return inputs, nil
}

func sourceParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(u.Host, "\\%") {
		return nil, fmt.Errorf("来源必须是不含账号密码的公共 HTTP(S) URL")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("来源 URL 的端口无效")
		}
	}
	for key := range u.Query() {
		normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
		if strings.Contains(normalized, "token") || strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "signature") || strings.Contains(normalized, "credential") || normalized == "key" || normalized == "apikey" || normalized == "authorization" {
			return nil, fmt.Errorf("来源 URL 不能包含密钥、令牌或签名参数")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || !strings.Contains(host, ".") && net.ParseIP(host) == nil || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home") {
		return nil, fmt.Errorf("不能导入本机或私有网络 URL")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !sourcePublicIP(ip) {
		return nil, fmt.Errorf("不能导入本机、私有或保留网络 URL")
	}
	u.Fragment = ""
	return u, nil
}

func sourcePublicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, raw := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16", "fec0::/10",
	} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

func newSourceHTTPClient() *http.Client {
	return &http.Client{Transport: &sourceHTTPTransport{}, Timeout: 45 * time.Second}
}

func (s *ImportService) sourceFetchURL(ctx context.Context, raw string) ([]byte, string, string, error) {
	client := s.sourceImports.httpClient
	if client == nil {
		client = newSourceHTTPClient()
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copyClient.Jar = nil
	if copyClient.Timeout == 0 || copyClient.Timeout > 45*time.Second {
		copyClient.Timeout = 45 * time.Second
	}
	for redirects := 0; redirects <= 3; redirects++ {
		u, err := sourceParseURL(raw)
		if err != nil {
			return nil, "", "", err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, "", "", fmt.Errorf("无法构造来源请求")
		}
		request.Header.Set("User-Agent", "NoteVault-SourceImport/1.0")
		request.Header.Set("Accept", "text/html,text/plain,application/zip,application/pdf,*/*;q=0.5")
		response, err := copyClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", "", ctx.Err()
			}
			var safe sourceNetworkError
			if errors.As(err, &safe) {
				return nil, "", "", safe
			}
			return nil, "", "", fmt.Errorf("无法获取来源页面；请检查公共 URL 或稍后重试")
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location := response.Header.Get("Location")
			_ = response.Body.Close()
			next, err := u.Parse(location)
			if err != nil || location == "" {
				return nil, "", "", fmt.Errorf("来源返回无效跳转")
			}
			raw = next.String()
			continue
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return nil, "", "", fmt.Errorf("来源返回 HTTP %d；仅支持可公开访问的资料", response.StatusCode)
		}
		if response.ContentLength > sourceMaxFileBytes {
			_ = response.Body.Close()
			return nil, "", "", fmt.Errorf("HTTP 来源超过 20 MiB 上限")
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, sourceMaxFileBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(data) > sourceMaxFileBytes {
			return nil, "", "", fmt.Errorf("HTTP 来源读取失败或超过 20 MiB 上限")
		}
		return data, response.Header.Get("Content-Type"), u.String(), nil
	}
	return nil, "", "", fmt.Errorf("来源跳转超过 3 次上限")
}

func sourceURLName(u *url.URL) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	name := u.Hostname()
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		name = parts[len(parts)-1]
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune("/\\:\x00<>\"|?*", r) || r < 32 {
			return '-'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "网页资料"
	}
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	return name
}

func sourceURLOrigin(u *url.URL) string {
	copyURL := *u
	copyURL.RawQuery, copyURL.Fragment, copyURL.User = "", "", nil
	origin := copyURL.String()
	if u.RawQuery != "" {
		origin += "#query-sha256=" + sourceChecksum([]byte(u.RawQuery))
	}
	return origin
}

func (s *ImportService) sourceFetchInputs(ctx context.Context, raw string) ([]sourceImportInput, error) {
	u, err := sourceParseURL(raw)
	if err != nil {
		return nil, err
	}
	origin := sourceURLOrigin(u)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if strings.EqualFold(u.Hostname(), "github.com") && len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		owner, repository := parts[0], strings.TrimSuffix(parts[1], ".git")
		if _, err := sourceRelativePath(owner + "/" + repository); err != nil {
			return nil, fmt.Errorf("GitHub 仓库地址无效")
		}
		archiveURL := "https://codeload.github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/zip/HEAD"
		data, _, _, err := s.sourceFetchURL(ctx, archiveURL)
		if err != nil {
			return nil, err
		}
		inputs, err := sourceReadArchive(ctx, data, &sourceImportBudget{})
		if err != nil {
			return nil, err
		}
		for i := range inputs {
			parts := strings.SplitN(inputs[i].name, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("GitHub 压缩包目录结构无效")
			}
			inputs[i].name, inputs[i].origin = parts[1], origin
		}
		sort.Slice(inputs, func(i, j int) bool { return inputs[i].name < inputs[j].name })
		return inputs, nil
	}
	data, contentType, finalURL, err := s.sourceFetchURL(ctx, u.String())
	if err != nil {
		return nil, err
	}
	name := path.Base(u.Path)
	if _, err := sourceRelativePath(name); err != nil || name == "." {
		name = "page"
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch mediaType {
	case "text/html", "application/xhtml+xml":
		name = strings.TrimSuffix(name, path.Ext(name)) + ".html"
	case "application/pdf":
		name = strings.TrimSuffix(name, path.Ext(name)) + ".pdf"
	case "text/plain":
		if !isMarkdownFile(name) {
			name = strings.TrimSuffix(name, path.Ext(name)) + ".txt"
		}
	default:
		if path.Ext(name) == "" {
			name += ".html"
		}
	}
	return []sourceImportInput{{name: name, origin: origin, baseURL: finalURL, data: data}}, nil
}

package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/core"
)

// FileNode 表示文件树中的一个节点（文件或文件夹）
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`     // 相对于工作区根目录的路径
	FullPath string      `json:"fullPath"` // 绝对路径
	IsDir    bool        `json:"isDir"`
	Children []*FileNode `json:"children,omitempty"`
	Size     int64       `json:"size,omitempty"`
	ModTime  string      `json:"modTime,omitempty"`
}

// snapshotCapturer 是 FileService 在破坏性写入前留存旧版本的钩子（P1-2）。
//
// 为什么用接口而不是直接持有 *SnapshotService：
//  1. 测试里可注入假实现，断言「保存前确实留了快照」而不碰真实磁盘；
//  2. 方法故意保持非导出——它只在 service 包内部使用，
//     若导出会被 Wails 反射绑定成前端 API，并触发 ports 契约测试报漂移。
type snapshotCapturer interface {
	captureBeforeWrite(workspacePath, relativePath string) (*Snapshot, error)
	captureBeforeDelete(workspacePath, relativePath string) (*Snapshot, error)
}

// FileService 管理文件的读取、创建、保存和删除
type FileService struct {
	// history 为 nil 时完全不留历史，功能不残废（红线：依赖必须可选可降级）
	history snapshotCapturer
}

// NewFileService 创建文件服务实例（不留版本历史）
func NewFileService() *FileService {
	return &FileService{}
}

// NewFileServiceWithHistory 创建带版本历史的文件服务实例。
//
// 用构造器注入而非 setter：setter 是导出方法，会被 Wails 绑定成
// 一个参数为 Go 接口的前端 API（生成的 TS 毫无意义），且违反 ports 契约。
func NewFileServiceWithHistory(history snapshotCapturer) *FileService {
	return &FileService{history: history}
}

// captureHistory 尽力留存一份旧版本。
//
// 关键约定：**历史留存失败绝不阻断用户的保存/删除**。
// 快照是保险，不是主链路；因为写不进 .notevault 就拒绝用户保存笔记，
// 是本末倒置的失败模式。
func (s *FileService) captureHistory(workspacePath, relativePath string, deleting bool) {
	if s.history == nil {
		return
	}
	var err error
	if deleting {
		_, err = s.history.captureBeforeDelete(workspacePath, relativePath)
	} else {
		_, err = s.history.captureBeforeWrite(workspacePath, relativePath)
	}
	if err != nil {
		log.Printf("[snapshot] 留存历史版本失败 path=%s deleting=%v: %v", relativePath, deleting, err)
	}
}

// confineToWorkspace 校验相对路径并拼接出工作区内的绝对路径。
// 阻断路径穿越：relativePath 为绝对路径（如 "C:\Windows\x"）或含 ".."
// 时 filepath.Join 会丢弃工作区前缀，导致读写删越界到工作区之外。
func confineToWorkspace(workspacePath, relativePath string) (string, error) {
	clean := filepath.Clean(relativePath)
	if filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", core.WrapError(core.ErrInvalidInput, "路径越出工作区: "+relativePath, nil)
	}
	return filepath.Join(workspacePath, clean), nil
}

// GetFileTree 获取指定工作区的文件树
// workspacePath: 工作区根目录绝对路径
// 返回文件树的根节点列表
func (s *FileService) GetFileTree(workspacePath string) ([]*FileNode, error) {
	return s.readDir(workspacePath, "")
}

// readDir 递归读取目录内容
func (s *FileService) readDir(rootPath string, relPath string) ([]*FileNode, error) {
	fullPath := filepath.Join(rootPath, relPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, core.OsToNVError(err, "读取目录失败: "+fullPath)
	}

	var nodes []*FileNode

	for _, entry := range entries {
		name := entry.Name()

		// 忽略隐藏文件和目录
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryRelPath := filepath.Join(relPath, name)
		entryFullPath := filepath.Join(rootPath, entryRelPath)

		node := &FileNode{
			Name:     name,
			Path:     filepath.ToSlash(entryRelPath),
			FullPath: entryFullPath,
			IsDir:    entry.IsDir(),
		}
		// best-effort stat：Info() 失败不应丢掉整个子树（旧实现直接 continue，
		// 目录条目会连同其全部内容从文件树中消失）——保留节点，size/modTime 留空。
		if info, err := entry.Info(); err == nil {
			node.Size = info.Size()
			node.ModTime = info.ModTime().Format(time.RFC3339)
		} else {
			log.Printf("[FileService] stat %s 失败: %v（size/modTime 未知）", entryFullPath, err)
		}

		if entry.IsDir() {
			// 递归读取子目录
			children, err := s.readDir(rootPath, entryRelPath)
			if err == nil {
				node.Children = children
			}
			nodes = append(nodes, node)
		} else {
			// 只包含笔记类文件：.md 是正文，.canvas 是 Canvas 白板（P2-3）。
			// 画布列表页靠 GetFileTree 收集 .canvas，漏掉会导致
			// 「新建画布提示已存在但列表永远为空」。
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".md" || ext == ".markdown" || ext == ".canvas" {
				nodes = append(nodes, node)
			}
		}
	}

	// 排序：文件夹在前，文件在后，按名称字母排序
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})

	return nodes, nil
}

// ReadFile 读取文件内容
// workspacePath: 工作区根目录
// relativePath: 相对于工作区根目录的文件路径
func (s *FileService) ReadFile(workspacePath string, relativePath string) (string, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", core.OsToNVError(err, "读取文件失败: "+relativePath)
	}
	return string(data), nil
}

// CreateFile 创建新文件
// workspacePath: 工作区根目录
// relativePath: 相对于工作区根目录的文件路径（含文件名）
// content: 文件初始内容
func (s *FileService) CreateFile(workspacePath string, relativePath string, content string) (*FileNode, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return nil, err
	}

	// 确保父目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, core.OsToNVError(err, "创建父目录失败: "+dir)
	}

	// 原子创建（O_CREATE|O_EXCL）：消除旧实现 Stat→WriteFile 的 TOCTOU 竞口
	// （并发创建同名文件时，后者可能静默覆盖前者）。
	// 保留底层 os 错误为 Cause：NVError.Error() 会渲染出 "The file exists."，
	// 前端既可按 ErrorCode 分支，也兼容既有的 message.includes('exist') 判定。
	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, core.WrapError(core.ErrAlreadyExists, "文件已存在: "+relativePath, err)
		}
		return nil, core.OsToNVError(err, "创建文件失败: "+relativePath)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return nil, core.OsToNVError(err, "写入文件失败: "+relativePath)
	}
	if err := f.Close(); err != nil {
		return nil, core.OsToNVError(err, "关闭文件失败: "+relativePath)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, core.OsToNVError(err, "读取文件信息失败: "+relativePath)
	}

	return &FileNode{
		Name:     filepath.Base(relativePath),
		Path:     filepath.ToSlash(relativePath),
		FullPath: fullPath,
		IsDir:    false,
		Size:     info.Size(),
		ModTime:  info.ModTime().Format(time.RFC3339),
	}, nil
}

// SaveFile 保存文件内容（覆盖写入）
// workspacePath: 工作区根目录
// relativePath: 相对于工作区根目录的文件路径
// content: 文件内容
func (s *FileService) SaveFile(workspacePath string, relativePath string, content string) error {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return err
	}

	// 确保父目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return core.OsToNVError(err, "创建父目录失败: "+dir)
	}

	// 覆盖前留存旧版本：SaveFile 是所有笔记写入的唯一收口，
	// 挂在这里才能保证没有一条写路径绕过版本历史。
	s.captureHistory(workspacePath, relativePath, false)

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return core.OsToNVError(err, "保存文件失败: "+relativePath)
	}
	return nil
}

// DeleteFile 删除文件（直接删除，不进回收站）
// workspacePath: 工作区根目录
// relativePath: 相对于工作区根目录的文件路径
func (s *FileService) DeleteFile(workspacePath string, relativePath string) error {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return err
	}
	// 这是"不进回收站"的硬删除路径，正是最需要版本历史兜底的地方
	s.captureHistory(workspacePath, relativePath, true)

	if err := os.Remove(fullPath); err != nil {
		return core.OsToNVError(err, "删除文件失败: "+relativePath)
	}
	return nil
}

// RenameFile 重命名文件或文件夹
// workspacePath: 工作区根目录
// oldRelativePath: 旧的相对路径
// newName: 新名称（不含路径）
func (s *FileService) RenameFile(workspacePath string, oldRelativePath string, newName string) (*FileNode, error) {
	oldFullPath, err := confineToWorkspace(workspacePath, oldRelativePath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(oldRelativePath)
	newRelativePath := filepath.Join(dir, newName)
	newFullPath, err := confineToWorkspace(workspacePath, newRelativePath)
	if err != nil {
		return nil, err
	}

	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		return nil, core.OsToNVError(err, "重命名失败: "+oldRelativePath)
	}

	info, err := os.Stat(newFullPath)
	if err != nil {
		return nil, core.OsToNVError(err, "读取文件信息失败: "+newRelativePath)
	}

	return &FileNode{
		Name:     newName,
		Path:     filepath.ToSlash(newRelativePath),
		FullPath: newFullPath,
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		ModTime:  info.ModTime().Format(time.RFC3339),
	}, nil
}

// CreateFolder 创建文件夹
// workspacePath: 工作区根目录
// relativePath: 相对于工作区根目录的文件夹路径
func (s *FileService) CreateFolder(workspacePath string, relativePath string) (*FileNode, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(fullPath, 0750); err != nil {
		return nil, core.OsToNVError(err, "创建文件夹失败: "+relativePath)
	}

	return &FileNode{
		Name:     filepath.Base(relativePath),
		Path:     filepath.ToSlash(relativePath),
		FullPath: fullPath,
		IsDir:    true,
	}, nil
}

// SaveImage 保存图片到工作区的 assets 文件夹
// workspacePath: 工作区根目录
// fileName: 图片文件名
// data: 图片二进制数据
// 返回图片相对于工作区的路径
func (s *FileService) SaveImage(workspacePath string, fileName string, data []byte) (string, error) {
	assetsDir := filepath.Join(workspacePath, "assets")
	if err := os.MkdirAll(assetsDir, 0750); err != nil {
		return "", core.OsToNVError(err, "创建 assets 目录失败: "+assetsDir)
	}

	// 生成唯一文件名（避免重名），用纳秒级时间戳 + 随机数
	// 先取 Base：fileName 可能含 "../../x.png" 之类的目录成分，
	// 直接拼接会让写入路径逃出 assets 目录。
	fileName = filepath.Base(fileName)
	ext := filepath.Ext(fileName)
	baseName := strings.TrimSuffix(fileName, ext)
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	uniqueName := fmt.Sprintf("%s_%d_%s%s", baseName, time.Now().UnixNano(), randomStr, ext)

	fullPath := filepath.Join(assetsDir, uniqueName)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return "", core.OsToNVError(err, "保存图片失败: "+uniqueName)
	}

	return filepath.ToSlash(filepath.Join("assets", uniqueName)), nil
}

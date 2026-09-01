package service

import (
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/notevault/notevault/internal/infra/schema"
)

// Reminder 表示一个提醒
type Reminder struct {
	ID        string `json:"id"`
	FilePath  string `json:"filePath"`
	FileName  string `json:"fileName"`
	Content   string `json:"content"`
	RemindAt  string `json:"remindAt"` // ISO 8601 格式
	CreatedAt string `json:"createdAt"`
	Completed bool   `json:"completed"`
}

// ReminderService 提供提醒管理功能
type ReminderService struct{}

// NewReminderService 创建提醒服务实例
func NewReminderService() *ReminderService {
	return &ReminderService{}
}

// getRemindersFilePath 获取提醒文件路径
func (s *ReminderService) getRemindersFilePath(workspacePath string) string {
	return filepath.Join(workspacePath, ".notevault", "reminders.json")
}

// loadReminders 加载所有提醒
func (s *ReminderService) loadReminders(workspacePath string) ([]*Reminder, error) {
	filePath := s.getRemindersFilePath(workspacePath)

	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	// 如果文件不存在，返回空列表
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return []*Reminder{}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 统一版本信封（E-7）。提醒是用户数据：旧的裸数组格式必须继续能读，
	// 否则升级一次等于所有提醒静默消失。
	reminders, res, err := schema.UnmarshalAs[[]*Reminder](data, schema.Reminders)
	if err != nil {
		return nil, err
	}
	if res.Compat == schema.CompatNewer {
		log.Printf("[reminder] 索引 schemaVersion=%d 高于当前支持的 %d，按当前结构尽力解析",
			res.FileVersion, schema.Reminders.Version)
	}
	if reminders == nil {
		reminders = []*Reminder{}
	}

	return reminders, nil
}

// saveReminders 保存提醒列表
func (s *ReminderService) saveReminders(workspacePath string, reminders []*Reminder) error {
	filePath := s.getRemindersFilePath(workspacePath)

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	if reminders == nil {
		reminders = []*Reminder{}
	}
	data, err := schema.MarshalAs(schema.Reminders, reminders)
	if err != nil {
		return err
	}

	// 原子写：崩溃时不留半截 JSON（否则提醒索引丢失）
	return atomicWrite(filePath, data, 0644)
}

// GetAllReminders 获取所有提醒
func (s *ReminderService) GetAllReminders(workspacePath string) ([]*Reminder, error) {
	reminders, err := s.loadReminders(workspacePath)
	if err != nil {
		return nil, err
	}

	// 按提醒时间排序，未完成的在前
	sort.Slice(reminders, func(i, j int) bool {
		if reminders[i].Completed != reminders[j].Completed {
			return !reminders[i].Completed
		}
		return reminders[i].RemindAt < reminders[j].RemindAt
	})

	return reminders, nil
}

// AddReminder 添加新提醒
func (s *ReminderService) AddReminder(workspacePath string, filePath string, content string, remindAt string) (*Reminder, error) {
	reminders, err := s.loadReminders(workspacePath)
	if err != nil {
		return nil, err
	}

	reminder := &Reminder{
		ID:        "rem_" + time.Now().Format("20060102150405") + "_" + randomString(6),
		FilePath:  filePath,
		FileName:  filepath.Base(filePath),
		Content:   content,
		RemindAt:  remindAt,
		CreatedAt: time.Now().Format(time.RFC3339),
		Completed: false,
	}

	reminders = append(reminders, reminder)

	if err := s.saveReminders(workspacePath, reminders); err != nil {
		return nil, err
	}

	return reminder, nil
}

// DeleteReminder 删除提醒
func (s *ReminderService) DeleteReminder(workspacePath string, id string) error {
	reminders, err := s.loadReminders(workspacePath)
	if err != nil {
		return err
	}

	for i, r := range reminders {
		if r.ID == id {
			reminders = append(reminders[:i], reminders[i+1:]...)
			break
		}
	}

	return s.saveReminders(workspacePath, reminders)
}

// ToggleReminder 切换提醒完成状态
func (s *ReminderService) ToggleReminder(workspacePath string, id string) (*Reminder, error) {
	reminders, err := s.loadReminders(workspacePath)
	if err != nil {
		return nil, err
	}

	var updated *Reminder
	for _, r := range reminders {
		if r.ID == id {
			r.Completed = !r.Completed
			updated = r
			break
		}
	}

	if updated == nil {
		return nil, nil
	}

	if err := s.saveReminders(workspacePath, reminders); err != nil {
		return nil, err
	}

	return updated, nil
}

// GetUpcomingReminders 获取即将到来的提醒（未来 24 小时内）
func (s *ReminderService) GetUpcomingReminders(workspacePath string) ([]*Reminder, error) {
	reminders, err := s.GetAllReminders(workspacePath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	cutoff := now.Add(24 * time.Hour)

	var upcoming []*Reminder
	for _, r := range reminders {
		if r.Completed {
			continue
		}
		remindTime, err := time.Parse(time.RFC3339, r.RemindAt)
		if err != nil {
			continue
		}
		if remindTime.After(now) && remindTime.Before(cutoff) {
			upcoming = append(upcoming, r)
		}
	}

	return upcoming, nil
}

// randomString 生成随机字符串（使用 math/rand 避免纳秒碰撞）
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

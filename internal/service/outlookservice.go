package service

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// OutlookService（Outlook 摄取一期）：只读日历会议
//
// 定位与边界：只「摄取」不「管理」——收发/回复/归档留在 Outlook，
// 这里只把会议变成今日页条带、会议纪要模板与日报/周报素材。
// 数据源是本机桌面版 Outlook 的 COM 接口（outlookservice_windows.go），
// 非 Windows 平台返回明确错误（outlookservice_other.go）。
//
// COM 读取失败（未装 Outlook / 对象被占用）一律降级为错误返回，
// 前端据此隐藏会议条带，绝不阻断日报/周报的生成主链路。
// ---------------------------------------------------------------------------

// OutlookMeeting 是一次日历会议的只读投影。
type OutlookMeeting struct {
	Subject   string `json:"subject"`
	Start     string `json:"start"` // 本地时间 YYYY-MM-DD HH:mm
	End       string `json:"end"`
	Location  string `json:"location"`
	Organizer string `json:"organizer"`
	AllDay    bool   `json:"allDay"`
}

// OutlookEmail 是一封旗标邮件的只读投影。
type OutlookEmail struct {
	Subject      string `json:"subject"`
	Sender       string `json:"sender"`
	ReceivedTime string `json:"receivedTime"` // 本地时间 YYYY-MM-DD HH:mm
	EntryID      string `json:"entryId"`
	Excerpt      string `json:"excerpt"` // 正文摘要（≤2000 字符）
}

// outlookProvider 抽象 Outlook 数据源（日历 + 邮件）：Windows 走 COM 实现，测试注入桩。
type outlookProvider interface {
	list(start, end time.Time) ([]OutlookMeeting, error)
	listFlagged(limit int) ([]OutlookEmail, error)
}

// OutlookService 提供只读 Outlook 数据查询。
type OutlookService struct {
	provider outlookProvider
}

// NewOutlookService 构造 Outlook 服务（生产用，平台决定数据源）。
func NewOutlookService() *OutlookService {
	return NewOutlookServiceWithProvider(nil)
}

// NewOutlookServiceWithProvider 允许注入自定义数据源（测试用）。
func NewOutlookServiceWithProvider(provider outlookProvider) *OutlookService {
	if provider == nil {
		provider = newOutlookProvider()
	}
	return &OutlookService{provider: provider}
}

// ListMeetings 返回 [start, end) 区间内的会议，按开始时间升序。
// start/end 为本地时间，接受 YYYY-MM-DD 或 YYYY-MM-DD HH:mm（含分钟粒度）。
func (s *OutlookService) ListMeetings(start, end string) ([]OutlookMeeting, error) {
	startTime, err := parseMeetingBoundary(start)
	if err != nil {
		return nil, fmt.Errorf("起始时间无效：%w", err)
	}
	endTime, err := parseMeetingBoundary(end)
	if err != nil {
		return nil, fmt.Errorf("结束时间无效：%w", err)
	}
	if !endTime.After(startTime) {
		return nil, fmt.Errorf("结束时间必须晚于起始时间：%s ≥ %s", end, start)
	}
	meetings, err := s.provider.list(startTime, endTime)
	if err != nil {
		return nil, err
	}
	if meetings == nil {
		meetings = []OutlookMeeting{}
	}
	sort.SliceStable(meetings, func(i, j int) bool { return meetings[i].Start < meetings[j].Start })
	return meetings, nil
}

// listFlaggedEmailsLimit 收录邮件单次拉取的默认/上限。
const (
	emailLimitDefault = 10
	emailLimitMax     = 50
)

// ListFlaggedEmails 返回收件箱中带旗标（用户标记为待办）的邮件，按接收时间倒序。
// limit <= 0 用默认 10，超过 50 截到 50。
func (s *OutlookService) ListFlaggedEmails(limit int) ([]OutlookEmail, error) {
	if limit <= 0 {
		limit = emailLimitDefault
	}
	if limit > emailLimitMax {
		limit = emailLimitMax
	}
	emails, err := s.provider.listFlagged(limit)
	if err != nil {
		return nil, err
	}
	if emails == nil {
		emails = []OutlookEmail{}
	}
	return emails, nil
}

// parseMeetingBoundary 解析日期键（零点）或日期时间键。
func parseMeetingBoundary(value string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02 15:04", value, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q 不是 YYYY-MM-DD 或 YYYY-MM-DD HH:mm 格式", value)
}

// meetingFileName 生成会议纪要的落盘文件名（主题中的文件系统非法字符已替换）。
func meetingFileName(date, subject string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		return r
	}, strings.TrimSpace(subject))
	if clean == "" {
		clean = "会议"
	}
	return "Daily/Meetings/" + date + "-" + clean + ".md"
}

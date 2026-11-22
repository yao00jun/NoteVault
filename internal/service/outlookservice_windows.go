//go:build windows

package service

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// outlookComProvider 通过本机桌面版 Outlook 的 COM 接口只读日历与邮件。
// 只调用 Outlook.Application → Session → GetDefaultFolder(…)，不写任何数据；
// Outlook 未安装 / 对象被占用时返回错误，由上层降级。
type outlookComProvider struct{}

// Outlook 默认文件夹枚举值。
const (
	olFolderCalendar = 9
	olFolderInbox    = 6
)

func newOutlookProvider() outlookProvider { return outlookComTimeoutProvider{} }

// outlookComTimeoutProvider 给 COM 调用加超时护栏：Outlook 有未处理完的
// 首启对话框 / 配置向导时，COM 调用会无限阻塞（本机实测踩坑）——服务层
// 必须在有限时间内返回，让前端降级而不是跟着卡死。超时后 COM goroutine
// 会残留（Go 无法强杀），但数量有界（每次超时一个），且不阻塞应用主流程。
type outlookComTimeoutProvider struct{}

const outlookComTimeout = 20 * time.Second

// withOutlookTimeout 统一的超时包装（泛型：会议与邮件共用）。
func withOutlookTimeout[T any](fn func() (T, error)) (T, error) {
	type result[T any] struct {
		value T
		err   error
	}
	done := make(chan result[T], 1) // 带缓冲：超时后 COM goroutine 写入也不阻塞
	go func() {
		value, err := fn()
		done <- result[T]{value, err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-time.After(outlookComTimeout):
		var zero T
		//nolint:staticcheck // Outlook 是专有名词，错误串以大写开头沿用既有口径
		return zero, fmt.Errorf("Outlook 数据读取超时（%s）：请确认桌面版 Outlook 已启动且没有未处理的首启对话框", outlookComTimeout)
	}
}

func (p outlookComTimeoutProvider) list(start, end time.Time) ([]OutlookMeeting, error) {
	return withOutlookTimeout(func() ([]OutlookMeeting, error) {
		return outlookComProvider{}.list(start, end)
	})
}

func (p outlookComTimeoutProvider) listFlagged(limit int) ([]OutlookEmail, error) {
	return withOutlookTimeout(func() ([]OutlookEmail, error) {
		return outlookComProvider{}.listFlagged(limit)
	})
}

func (p outlookComTimeoutProvider) emailBody(entryID string) (string, error) {
	return withOutlookTimeout(func() (string, error) {
		return outlookComProvider{}.emailBody(entryID)
	})
}

func (outlookComProvider) list(start, end time.Time) ([]OutlookMeeting, error) {
	// COM 要求每线程初始化；锁住 OS 线程避免 goroutine 调度把调用挪到未初始化线程
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitialize(0); err != nil {
		return nil, fmt.Errorf("COM 初始化失败：%w", err)
	}
	defer ole.CoUninitialize()

	appUnknown, err := oleutil.CreateObject("Outlook.Application")
	if err != nil {
		return nil, fmt.Errorf("无法连接 Outlook（请确认已安装并启动桌面版）：%w", err)
	}
	defer appUnknown.Release()
	app, err := appUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, fmt.Errorf("连接 Outlook 失败：%w", err)
	}
	defer app.Release()

	sessionRaw, err := oleutil.GetProperty(app, "Session")
	if err != nil {
		return nil, fmt.Errorf("获取 Outlook 会话失败：%w", err)
	}
	session := sessionRaw.ToIDispatch()
	defer session.Release()

	calendarRaw, err := oleutil.GetProperty(session, "GetDefaultFolder", olFolderCalendar)
	if err != nil {
		return nil, fmt.Errorf("获取日历文件夹失败：%w", err)
	}
	calendar := calendarRaw.ToIDispatch()
	defer calendar.Release()

	itemsRaw, err := oleutil.GetProperty(calendar, "Items")
	if err != nil {
		return nil, fmt.Errorf("获取日历条目失败：%w", err)
	}
	items := itemsRaw.ToIDispatch()
	defer items.Release()

	// Restrict 用本地时间字符串过滤；yyyy-MM-dd HH:mm 在各语言版本下均可解析
	filter := fmt.Sprintf("[Start] >= '%s' AND [End] <= '%s'",
		start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"))
	restrictedRaw, err := oleutil.CallMethod(items, "Restrict", filter)
	if err != nil {
		return nil, fmt.Errorf("过滤日历失败：%w", err)
	}
	restricted := restrictedRaw.ToIDispatch()
	defer restricted.Release()

	countRaw, err := oleutil.GetProperty(restricted, "Count")
	if err != nil {
		return nil, fmt.Errorf("读取会议数量失败：%w", err)
	}
	count := int(countRaw.Val)

	meetings := make([]OutlookMeeting, 0, count)
	for i := 1; i <= count; i++ {
		itemRaw, err := oleutil.GetProperty(restricted, "Item", i)
		if err != nil {
			continue // 单条失败不阻断整批
		}
		item := itemRaw.ToIDispatch()
		meeting, readErr := readMeeting(item)
		item.Release()
		if readErr != nil || meeting.Subject == "" {
			continue
		}
		meetings = append(meetings, *meeting)
	}
	return meetings, nil
}

// readMeeting 读取单条 AppointmentItem 的展示字段。
// 属性读取全部走安全版本（GetProperty + err），异常字段降级为空值，
// 绝不让单个 COM 异常 panic 掉整个调用。
func readMeeting(item *ole.IDispatch) (*OutlookMeeting, error) {
	subject := outlookString(item, "Subject")
	location := outlookString(item, "Location")
	organizer := outlookString(item, "Organizer")
	allDay := outlookBool(item, "AllDayEvent")
	attendees := append(readAttendeeNames(item, "RequiredAttendees"), readAttendeeNames(item, "OptionalAttendees")...)

	startRaw, err := oleutil.GetProperty(item, "Start")
	if err != nil {
		return nil, fmt.Errorf("读取会议开始时间失败：%w", err)
	}
	start, err := ole.GetVariantDate(uint64(startRaw.Val))
	if err != nil {
		return nil, fmt.Errorf("解析会议开始时间失败：%w", err)
	}
	endRaw, err := oleutil.GetProperty(item, "End")
	if err != nil {
		return nil, fmt.Errorf("读取会议结束时间失败：%w", err)
	}
	end, err := ole.GetVariantDate(uint64(endRaw.Val))
	if err != nil {
		return nil, fmt.Errorf("解析会议结束时间失败：%w", err)
	}
	return &OutlookMeeting{
		Subject:   strings.TrimSpace(subject),
		Start:     start.Format("2006-01-02 15:04"),
		End:       end.Format("2006-01-02 15:04"),
		Location:  strings.TrimSpace(location),
		Organizer: strings.TrimSpace(organizer),
		Attendees: attendees,
		AllDay:    allDay,
	}, nil
}

// outlookString 安全读取字符串属性，异常返回空串。
func outlookString(item *ole.IDispatch, name string) string {
	raw, err := oleutil.GetProperty(item, name)
	if err != nil {
		return ""
	}
	return raw.ToString()
}

// outlookBool 安全读取布尔属性，异常返回 false。
func outlookBool(item *ole.IDispatch, name string) bool {
	raw, err := oleutil.GetProperty(item, name)
	if err != nil {
		return false
	}
	return raw.Val != 0
}

// listFlagged 读取收件箱中带旗标（用户标记为待办）的邮件，按接收时间倒序。
// 旗标邮件是用户显式标记的动作项，信号强度远高于未读，故一期只取旗标。
func (outlookComProvider) listFlagged(limit int) ([]OutlookEmail, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitialize(0); err != nil {
		return nil, fmt.Errorf("COM 初始化失败：%w", err)
	}
	defer ole.CoUninitialize()

	appUnknown, err := oleutil.CreateObject("Outlook.Application")
	if err != nil {
		return nil, fmt.Errorf("无法连接 Outlook（请确认已安装并启动桌面版）：%w", err)
	}
	defer appUnknown.Release()
	app, err := appUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, fmt.Errorf("连接 Outlook 失败：%w", err)
	}
	defer app.Release()

	sessionRaw, err := oleutil.GetProperty(app, "Session")
	if err != nil {
		return nil, fmt.Errorf("获取 Outlook 会话失败：%w", err)
	}
	session := sessionRaw.ToIDispatch()
	defer session.Release()

	inboxRaw, err := oleutil.GetProperty(session, "GetDefaultFolder", olFolderInbox)
	if err != nil {
		return nil, fmt.Errorf("获取收件箱失败：%w", err)
	}
	inbox := inboxRaw.ToIDispatch()
	defer inbox.Release()

	itemsRaw, err := oleutil.GetProperty(inbox, "Items")
	if err != nil {
		return nil, fmt.Errorf("获取邮件条目失败：%w", err)
	}
	items := itemsRaw.ToIDispatch()
	defer items.Release()

	// olFlagMarked = 2：用户打了旗标待办的邮件
	restrictedRaw, err := oleutil.CallMethod(items, "Restrict", "[FlagStatus] = 2")
	if err != nil {
		return nil, fmt.Errorf("过滤旗标邮件失败：%w", err)
	}
	restricted := restrictedRaw.ToIDispatch()
	defer restricted.Release()

	// 按接收时间倒序，取前 limit 封（Sort 返回空的 VARIANT，显式释放）
	sortRaw, err := oleutil.CallMethod(restricted, "Sort", "[ReceivedTime]", true)
	if err != nil {
		return nil, fmt.Errorf("邮件排序失败：%w", err)
	}
	_ = sortRaw.Clear()

	countRaw, err := oleutil.GetProperty(restricted, "Count")
	if err != nil {
		return nil, fmt.Errorf("读取邮件数量失败：%w", err)
	}
	count := int(countRaw.Val)
	if count > limit {
		count = limit
	}

	emails := make([]OutlookEmail, 0, count)
	for i := 1; i <= count; i++ {
		itemRaw, err := oleutil.GetProperty(restricted, "Item", i)
		if err != nil {
			continue
		}
		item := itemRaw.ToIDispatch()
		email, readErr := readEmail(item)
		item.Release()
		if readErr != nil {
			continue
		}
		emails = append(emails, *email)
	}
	return emails, nil
}

// emailExcerptLimit 邮件正文摘要的最大字符数。
const emailExcerptLimit = 2000

// readEmail 读取单封 MailItem 的展示字段与正文摘要。
func readEmail(item *ole.IDispatch) (*OutlookEmail, error) {
	subject := outlookString(item, "Subject")
	sender := outlookString(item, "SenderName")
	entryID := outlookString(item, "EntryID")
	body := outlookString(item, "Body")
	// 纯文本近似：压缩多余空行，截到摘要上限
	body = strings.Join(strings.Fields(body), "\n")

	receivedRaw, err := oleutil.GetProperty(item, "ReceivedTime")
	if err != nil {
		return nil, fmt.Errorf("读取邮件接收时间失败：%w", err)
	}
	received, err := ole.GetVariantDate(uint64(receivedRaw.Val))
	if err != nil {
		return nil, fmt.Errorf("解析邮件接收时间失败：%w", err)
	}
	return &OutlookEmail{
		Subject:      strings.TrimSpace(subject),
		Sender:       strings.TrimSpace(sender),
		ReceivedTime: received.Format("2006-01-02 15:04"),
		EntryID:      entryID,
		Excerpt:      truncateRunes(body, emailExcerptLimit),
	}, nil
}

// readAttendeeNames 读取会议的参会人集合（RequiredAttendees / OptionalAttendees），
// 返回名字列表；集合读取失败降级为空列表（个别字段异常不影响整条会议）。
func readAttendeeNames(item *ole.IDispatch, property string) []string {
	recipientsRaw, err := oleutil.GetProperty(item, property)
	if err != nil {
		return nil
	}
	recipients := recipientsRaw.ToIDispatch()
	defer recipients.Release()

	countRaw, err := oleutil.GetProperty(recipients, "Count")
	if err != nil {
		return nil
	}
	count := int(countRaw.Val)
	names := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		recRaw, err := oleutil.GetProperty(recipients, "Item", i)
		if err != nil {
			continue
		}
		rec := recRaw.ToIDispatch()
		name := outlookString(rec, "Name")
		rec.Release()
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// emailBody 按 EntryID 读取邮件全文（不截断）。
// Namespace.GetItemFromID 是 Outlook 定位已有条目的标准方式；
// 邮件可能已被删除/移走，此时返回明确错误由前端降级为摘要。
func (outlookComProvider) emailBody(entryID string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitialize(0); err != nil {
		return "", fmt.Errorf("COM 初始化失败：%w", err)
	}
	defer ole.CoUninitialize()

	appUnknown, err := oleutil.CreateObject("Outlook.Application")
	if err != nil {
		return "", fmt.Errorf("无法连接 Outlook（请确认已安装并启动桌面版）：%w", err)
	}
	defer appUnknown.Release()
	app, err := appUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return "", fmt.Errorf("连接 Outlook 失败：%w", err)
	}
	defer app.Release()

	sessionRaw, err := oleutil.GetProperty(app, "Session")
	if err != nil {
		return "", fmt.Errorf("获取 Outlook 会话失败：%w", err)
	}
	session := sessionRaw.ToIDispatch()
	defer session.Release()

	itemRaw, err := oleutil.CallMethod(session, "GetItemFromID", entryID)
	if err != nil {
		return "", fmt.Errorf("定位邮件失败（可能已被删除或移动）：%w", err)
	}
	item := itemRaw.ToIDispatch()
	defer item.Release()

	body := outlookString(item, "Body")
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("邮件正文为空")
	}
	return body, nil
}

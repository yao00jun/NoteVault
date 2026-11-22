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

// outlookComProvider 通过本机桌面版 Outlook 的 COM 接口只读日历。
// 只调用 Outlook.Application → Session → GetDefaultFolder(9)（olFolderCalendar），
// 不写任何数据；Outlook 未安装 / 对象被占用时返回错误，由上层降级。
type outlookComProvider struct{}

// olFolderCalendar 是 Outlook 默认日历文件夹的枚举值。
const olFolderCalendar = 9

func newOutlookProvider() meetingProvider { return outlookComTimeoutProvider{} }

// outlookComTimeoutProvider 给 COM 调用加超时护栏：Outlook 有未处理完的
// 首启对话框 / 配置向导时，COM 调用会无限阻塞（本机实测踩坑）——服务层
// 必须在有限时间内返回，让前端降级而不是跟着卡死。超时后 COM goroutine
// 会残留（Go 无法强杀），但数量有界（每次超时一个），且不阻塞应用主流程。
type outlookComTimeoutProvider struct{}

const outlookComTimeout = 20 * time.Second

func (p outlookComTimeoutProvider) list(start, end time.Time) ([]OutlookMeeting, error) {
	type result struct {
		meetings []OutlookMeeting
		err      error
	}
	done := make(chan result, 1) // 带缓冲：超时后 COM goroutine 写入也不阻塞
	go func() {
		meetings, err := outlookComProvider{}.list(start, end)
		done <- result{meetings, err}
	}()
	select {
	case r := <-done:
		return r.meetings, r.err
	case <-time.After(outlookComTimeout):
		//nolint:staticcheck // Outlook 是专有名词，错误串以大写开头沿用既有口径
		return nil, fmt.Errorf("Outlook 日历读取超时（%s）：请确认桌面版 Outlook 已启动且没有未处理的首启对话框", outlookComTimeout)
	}
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

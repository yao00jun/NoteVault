package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeMeetingProvider 是 Outlook 数据源的测试桩：记录调用参数并返回固定结果。
type fakeMeetingProvider struct {
	meetings []OutlookMeeting
	emails   []OutlookEmail
	err      error
	body     string
	bodyErr  error
	gotStart time.Time
	gotEnd   time.Time
	gotLimit int
	gotEntry string
}

func (p *fakeMeetingProvider) list(start, end time.Time) ([]OutlookMeeting, error) {
	p.gotStart, p.gotEnd = start, end
	if p.err != nil {
		return nil, p.err
	}
	return p.meetings, nil
}

func (p *fakeMeetingProvider) emailBody(entryID string) (string, error) {
	p.gotEntry = entryID
	return p.body, p.bodyErr
}

func (p *fakeMeetingProvider) listFlagged(limit int) ([]OutlookEmail, error) {
	p.gotLimit = limit
	if p.err != nil {
		return nil, p.err
	}
	return p.emails, nil
}

func TestOutlookService_ListMeetingsSortsAndPassesWindow(t *testing.T) {
	fake := &fakeMeetingProvider{meetings: []OutlookMeeting{
		{Subject: "晚的", Start: "2026-11-23 16:00", End: "2026-11-23 17:00"},
		{Subject: "早的", Start: "2026-11-23 09:00", End: "2026-11-23 09:30"},
	}}
	svc := NewOutlookServiceWithProvider(fake)

	meetings, err := svc.ListMeetings("2026-11-23", "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}
	// 时间窗口按本地时区解析：日期键 = 零点
	if !fake.gotStart.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("start boundary should be midnight: %s", fake.gotStart)
	}
	if !fake.gotEnd.Equal(time.Date(2026, 11, 24, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("end boundary should be midnight: %s", fake.gotEnd)
	}
	// 按开始时间升序
	if len(meetings) != 2 || meetings[0].Subject != "早的" || meetings[1].Subject != "晚的" {
		t.Fatalf("meetings should be sorted by start: %+v", meetings)
	}
	// 日期时间键同样接受
	if _, err := svc.ListMeetings("2026-11-23 08:30", "2026-11-23 18:00"); err != nil {
		t.Fatal(err)
	}
	if !fake.gotStart.Equal(time.Date(2026, 11, 23, 8, 30, 0, 0, time.Local)) {
		t.Fatalf("datetime boundary should be honored: %s", fake.gotStart)
	}
}

func TestOutlookService_RejectsInvalidRange(t *testing.T) {
	svc := NewOutlookServiceWithProvider(&fakeMeetingProvider{})
	if _, err := svc.ListMeetings("2026/11/23", "2026-11-24"); err == nil {
		t.Fatal("invalid start format must be rejected")
	}
	if _, err := svc.ListMeetings("2026-11-24", "2026-11-23"); err == nil {
		t.Fatal("inverted range must be rejected")
	}
	if _, err := svc.ListMeetings("2026-11-23", "2026-11-23"); err == nil {
		t.Fatal("zero-length range must be rejected")
	}
}

func TestOutlookService_PropagatesProviderErrorAndDefaultsToPlatformProvider(t *testing.T) {
	providerErr := errors.New("outlook down")
	svc := NewOutlookServiceWithProvider(&fakeMeetingProvider{err: providerErr})
	if _, err := svc.ListMeetings("2026-11-23", "2026-11-24"); !errors.Is(err, providerErr) {
		t.Fatalf("provider error must propagate: %v", err)
	}
	// 未注入 provider 时落到平台实现（本机为 COM；CI 无 Outlook 时只会得到错误而非 panic）
	platform := NewOutlookService()
	if platform.provider == nil {
		t.Fatal("default provider must be set")
	}
}

func TestOutlookService_EmptyResultIsNeverNil(t *testing.T) {
	svc := NewOutlookServiceWithProvider(&fakeMeetingProvider{})
	meetings, err := svc.ListMeetings("2026-11-23", "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}
	if meetings == nil {
		t.Fatal("empty result must be an empty slice, not nil")
	}
}

func TestOutlookService_MeetingFileNameSanitizes(t *testing.T) {
	if got := meetingFileName("2026-11-23", ` 周会: 讨论/下一步 `); got != "Daily/Meetings/2026-11-23-周会- 讨论-下一步.md" {
		t.Fatalf("unexpected file name: %q", got)
	}
	if got := meetingFileName("2026-11-23", ""); got != "Daily/Meetings/2026-11-23-会议.md" {
		t.Fatalf("empty subject should fall back: %q", got)
	}
}

func TestOutlookService_ListFlaggedEmailsClampsLimitAndDefaults(t *testing.T) {
	fake := &fakeMeetingProvider{emails: []OutlookEmail{
		{Subject: "评审请示", Sender: "领导", ReceivedTime: "2026-11-23 10:00", Excerpt: "请今天回复"},
	}}
	svc := NewOutlookServiceWithProvider(fake)

	// limit <= 0 → 默认 10
	if _, err := svc.ListFlaggedEmails(0); err != nil {
		t.Fatal(err)
	}
	if fake.gotLimit != emailLimitDefault {
		t.Fatalf("zero limit should fall back to default 10, got %d", fake.gotLimit)
	}
	// limit > 50 → 截到 50
	if _, err := svc.ListFlaggedEmails(500); err != nil {
		t.Fatal(err)
	}
	if fake.gotLimit != emailLimitMax {
		t.Fatalf("oversized limit should clamp to 50, got %d", fake.gotLimit)
	}
	// 正常路径返回数据
	emails, err := svc.ListFlaggedEmails(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 1 || emails[0].Subject != "评审请示" || emails[0].Excerpt != "请今天回复" {
		t.Fatalf("unexpected emails: %+v", emails)
	}
}

func TestOutlookService_ListFlaggedEmailsPropagatesErrorAndNeverNil(t *testing.T) {
	providerErr := errors.New("outlook down")
	svc := NewOutlookServiceWithProvider(&fakeMeetingProvider{err: providerErr})
	if _, err := svc.ListFlaggedEmails(10); !errors.Is(err, providerErr) {
		t.Fatalf("provider error must propagate: %v", err)
	}
	svc2 := NewOutlookServiceWithProvider(&fakeMeetingProvider{})
	emails, err := svc2.ListFlaggedEmails(10)
	if err != nil {
		t.Fatal(err)
	}
	if emails == nil {
		t.Fatal("empty result must be an empty slice, not nil")
	}
}

func TestOutlookService_GetEmailBodyValidatesAndPropagates(t *testing.T) {
	fake := &fakeMeetingProvider{body: "全文正文"}
	svc := NewOutlookServiceWithProvider(fake)

	// 空 entryID 直接拒绝，不打到 provider
	if _, err := svc.GetEmailBody("  "); err == nil {
		t.Fatal("empty entryID must be rejected")
	}
	body, err := svc.GetEmailBody("ENTRY-1")
	if err != nil {
		t.Fatal(err)
	}
	if body != "全文正文" {
		t.Fatalf("unexpected body: %q", body)
	}
	if fake.gotEntry != "ENTRY-1" {
		t.Fatalf("entryID must be passed through, got %q", fake.gotEntry)
	}

	// provider 错误传播
	svc2 := NewOutlookServiceWithProvider(&fakeMeetingProvider{bodyErr: errors.New("邮件已被删除")})
	if _, err := svc2.GetEmailBody("ENTRY-2"); err == nil || !strings.Contains(err.Error(), "邮件已被删除") {
		t.Fatalf("provider error must propagate: %v", err)
	}
}

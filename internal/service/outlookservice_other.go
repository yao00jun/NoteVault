//go:build !windows

package service

import (
	"fmt"
	"time"
)

// outlookUnavailableProvider 在非 Windows 平台明确拒绝：
// 桌面版 Outlook COM 仅存在于 Windows，此错误由前端降级处理（隐藏会议/邮件条带）。
type outlookUnavailableProvider struct{}

func newOutlookProvider() outlookProvider { return outlookUnavailableProvider{} }

func (outlookUnavailableProvider) list(time.Time, time.Time) ([]OutlookMeeting, error) {
	//nolint:staticcheck // Outlook 是专有名词，错误串以大写开头沿用既有口径
	return nil, fmt.Errorf("Outlook 日历摄取仅支持 Windows（桌面版 Outlook 的 COM 接口）")
}

func (outlookUnavailableProvider) listFlagged(int) ([]OutlookEmail, error) {
	//nolint:staticcheck // Outlook 是专有名词，错误串以大写开头沿用既有口径
	return nil, fmt.Errorf("Outlook 邮件摄取仅支持 Windows（桌面版 Outlook 的 COM 接口）")

func (outlookUnavailableProvider) emailBody(string) (string, error) {
	//nolint:staticcheck // Outlook 是专有名词，错误串以大写开头沿用既有口径
	return "", fmt.Errorf("Outlook 邮件摄取仅支持 Windows（桌面版 Outlook 的 COM 接口）")
}

//go:build windows

package service

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

func sourceSystemProxySettings() (string, bool, error) {
	settings, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, sourceNetworkError("无法读取当前用户的系统代理配置")
	}
	defer settings.Close()
	enabled, _, err := settings.GetIntegerValue("ProxyEnable")
	if errors.Is(err, registry.ErrNotExist) || err == nil && enabled == 0 {
		return "", false, nil
	}
	if err != nil {
		return "", false, sourceNetworkError("当前用户的系统代理启用设置无效")
	}
	proxy, _, err := settings.GetStringValue("ProxyServer")
	if err != nil {
		return "", true, sourceNetworkError("系统代理已启用，但没有可读取的固定代理地址")
	}
	return proxy, true, nil
}

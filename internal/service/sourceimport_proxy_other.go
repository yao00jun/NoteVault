//go:build !windows

package service

func sourceSystemProxySettings() (string, bool, error) { return "", false, nil }

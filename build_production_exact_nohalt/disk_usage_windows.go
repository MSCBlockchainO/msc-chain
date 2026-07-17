//go:build windows

package main

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diskUsagePercent(path string) float64 {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	root := filepath.VolumeName(path)
	if root == "" {
		return 0
	}
	root += `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	var freeToCaller uint64
	var total uint64
	var totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(rootPtr, &freeToCaller, &total, &totalFree); err != nil || total == 0 {
		return 0
	}
	return (float64(total-totalFree) / float64(total)) * 100
}

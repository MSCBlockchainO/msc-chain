//go:build windows

package main

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskUsagePercent implements the disk usage percent helper.
func diskUsagePercent(path string) float64 {
	if path == "" {
		path = "."
	}
	// `abs` and `err` store the error produced by this operation.
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	// `root` stores the digest used to identify or verify the related data.
	root := filepath.VolumeName(path)
	if root == "" {
		return 0
	}
	root += `\`
	// `rootPtr` and `err` store the error produced by this operation.
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	// `freeToCaller` stores the value used by this operation.
	var freeToCaller uint64
	// `total` stores the measured quantity used by this operation.
	var total uint64
	// `totalFree` stores the measured quantity used by this operation.
	var totalFree uint64
	// `err` stores the error produced by this operation.
	if err := windows.GetDiskFreeSpaceEx(rootPtr, &freeToCaller, &total, &totalFree); err != nil || total == 0 {
		return 0
	}
	return (float64(total-totalFree) / float64(total)) * 100
}

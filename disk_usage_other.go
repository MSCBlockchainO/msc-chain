//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd

package main

// diskUsagePercent implements the disk usage percent helper.
func diskUsagePercent(path string) float64 {
	return 0
}

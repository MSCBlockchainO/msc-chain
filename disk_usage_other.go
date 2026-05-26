//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd

package main

func diskUsagePercent(path string) float64 {
	return 0
}

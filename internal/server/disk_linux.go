//go:build linux

package server

import "syscall"

// diskUsage смотрит на файловую систему, где лежит ~/edge — то есть на корень
// сервера. Своя, контейнерная, была бы бесполезна.
func diskUsage(path string) (totalGB, usedGB float64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	const gb = 1024 * 1024 * 1024
	total := float64(st.Blocks) * float64(st.Bsize) / gb
	free := float64(st.Bavail) * float64(st.Bsize) / gb
	return round1(total), round1(total - free)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

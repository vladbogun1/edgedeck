//go:build linux

package routes

import (
	"os"
	"syscall"
)

// ownerOf — uid/gid каталога, чтобы сгенерированные файлы принадлежали тому же
// пользователю, что и ~/edge (обычно ubuntu), а не root из контейнера.
func ownerOf(path string) (int, int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

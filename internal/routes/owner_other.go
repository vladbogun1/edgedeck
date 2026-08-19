//go:build !linux

package routes

// Панель всегда работает в linux-контейнере; файл нужен только чтобы сборка и
// go vet на Windows/macOS не падали при локальной разработке.
func ownerOf(string) (int, int, bool) { return 0, 0, false }

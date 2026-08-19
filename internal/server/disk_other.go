//go:build !linux

package server

// На не-Linux панель не запускается (она всегда в контейнере), но файл нужен,
// чтобы `go vet` и сборка под Windows/macOS не падали при локальной разработке.
func diskUsage(string) (totalGB, usedGB float64) { return 0, 0 }

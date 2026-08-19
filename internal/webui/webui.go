// Package webui — собранный интерфейс, вшитый в бинарник.
//
// В образе фронтенд собирается vite и кладётся сюда (см. Dockerfile), поэтому
// панель — один файл без внешних зависимостей: ни nginx рядом, ни каталога со
// статикой на сервере.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler отдаёт статику, а на неизвестные пути — index.html: маршрутизация
// внутри приложения клиентская, и обновление страницы на /ports должно работать.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	index, indexErr := fs.ReadFile(sub, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := sub.Open(path); err == nil {
				_ = f.Close()
				// Хеш в имени файла есть только у сборочных ассетов — их и кешируем.
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if indexErr != nil {
			http.Error(w, "интерфейс не собран", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(index)
	})
}

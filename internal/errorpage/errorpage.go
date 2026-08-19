// Package errorpage — страница «сервис не отвечает».
//
// Зачем она вообще: при перезапуске контейнера или его падении посетитель иначе
// видит голый текст «502 Bad Gateway» от прокси. Это выглядит как поломка сайта,
// хотя сайт настроен верно и вернётся сам через несколько секунд.
//
// Страница обязана работать именно тогда, когда всё сломано, поэтому в ней нет
// ни одного внешнего файла: ни шрифтов, ни картинок, ни скриптов. Код ответа и
// домен подставляет Caddy (директива templates), а если подстановка не сработала —
// страница берёт их из адресной строки.
package errorpage

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed error.html
var files embed.FS

// Dir — каталог внутри www, куда кладётся страница. Имя с подчёркиванием, чтобы
// не столкнуться с каталогом настоящего домена.
const Dir = "_edgedeck"

// FileName — как файл называется на диске и в конфиге Caddy.
const FileName = "error.html"

// Ensure кладёт страницу в www, если её там нет или она отличается от вшитой.
// Вызывается при старте панели: страница должна существовать раньше, чем
// понадобится, а не появляться в момент аварии.
//
// chown — та же причина, что и у остальных файлов: панель работает от root,
// а каталог ~/edge принадлежит ubuntu, и файлы должны оставаться доступными ему.
func Ensure(wwwDir string, chown func(path string, content []byte) error) error {
	want, err := files.ReadFile(FileName)
	if err != nil {
		return err
	}
	path := filepath.Join(wwwDir, Dir, FileName)
	if have, err := os.ReadFile(path); err == nil && string(have) == string(want) {
		return nil
	}
	return chown(path, want)
}

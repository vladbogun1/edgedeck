package server

import (
	"net/url"
	"testing"
)

func TestFillMeta(t *testing.T) {
	page := `<html><head><title> Магазин &amp; склад </title>
<meta name="description" content="Описание">
<meta property="og:image" content="/og.png">
<link rel="icon" href="/favicon.ico">
<link rel="apple-touch-icon" href="https://cdn.example.com/a.png">
<link rel="icon" href="javascript:alert(1)">
</head></html>`
	base, _ := url.Parse("https://shop.example.com/ru/")
	var m siteMeta
	fillMeta(&m, page, base)
	if m.Title != "Магазин & склад" || m.Description != "Описание" {
		t.Errorf("заголовок/описание: %+v", m)
	}
	if m.Image != "https://shop.example.com/og.png" {
		t.Errorf("картинка: %q", m.Image)
	}
	if m.Icon != "https://cdn.example.com/a.png" {
		t.Errorf("иконка: %q", m.Icon)
	}
}

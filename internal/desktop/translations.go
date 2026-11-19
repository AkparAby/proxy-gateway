//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/lang"
)

func init() {
	// Fill the locale alias used by Windows and keep toolkit dialogs consistent
	// with the application's Chinese labels.
	_ = lang.AddTranslationsForLocale([]byte(`{"(Select one)":"请选择","Cancel":"取消","Confirm":"确认","Error":"错误","OK":"确定","Yes":"是","No":"否","Quit":"退出"}`), fyne.Locale("zh-Hans"))
}

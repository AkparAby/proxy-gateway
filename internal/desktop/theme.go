//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"image/color"
	"local/proxy-gateway/assets"
)

type gatewayTheme struct{ font fyne.Resource }

func NewTheme() fyne.Theme {
	return &gatewayTheme{fyne.NewStaticResource("NotoSansSC-Regular.otf", assets.Font)}
}
func (t *gatewayTheme) Font(fyne.TextStyle) fyne.Resource { return t.font }
func (t *gatewayTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}
func (t *gatewayTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return 15
	}
	if name == theme.SizeNamePadding {
		return 8
	}
	return theme.DefaultTheme().Size(name)
}
func (t *gatewayTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 15, G: 23, B: 32, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 230, G: 237, B: 244, A: 255}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 57, G: 207, B: 157, A: 255}
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return color.NRGBA{R: 32, G: 45, B: 59, A: 255}
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}

package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// MyTheme 是取货雷达的外观。
//
// fyne 默认外观偏工具箱风格：方角、灰蓝按钮、紧凑留白。这里按 Apple 的
// 界面习惯重新给了一套颜色与尺寸 —— 取值直接来自 Apple 自家界面：
// 系统蓝 #0071E3、分隔线 #D2D2D7、近黑 #1D1D1F、浅灰底 #F5F5F7，
// 深色模式则用 #2997FF 与 #1D1D1F 这一组。
type MyTheme struct{}

var _ fyne.Theme = (*MyTheme)(nil)

// 浅色
var (
	lightBackground = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF7, A: 0xFF} // 窗口底
	lightSurface    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // 输入框、列表
	lightForeground = color.NRGBA{R: 0x1D, G: 0x1D, B: 0x1F, A: 0xFF}
	lightSecondary  = color.NRGBA{R: 0x6E, G: 0x6E, B: 0x73, A: 0xFF}
	lightSeparator  = color.NRGBA{R: 0xD2, G: 0xD2, B: 0xD7, A: 0xFF}
	lightPrimary    = color.NRGBA{R: 0x00, G: 0x71, B: 0xE3, A: 0xFF}
)

// 深色
var (
	darkBackground = color.NRGBA{R: 0x1D, G: 0x1D, B: 0x1F, A: 0xFF}
	darkSurface    = color.NRGBA{R: 0x2C, G: 0x2C, B: 0x2E, A: 0xFF}
	darkForeground = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF7, A: 0xFF}
	darkSecondary  = color.NRGBA{R: 0x98, G: 0x98, B: 0x9D, A: 0xFF}
	darkSeparator  = color.NRGBA{R: 0x42, G: 0x42, B: 0x45, A: 0xFF}
	darkPrimary    = color.NRGBA{R: 0x29, G: 0x97, B: 0xFF, A: 0xFF}
)

// 状态色在两种模式下通用，取自 Apple 的系统色
var (
	systemGreen  = color.NRGBA{R: 0x34, G: 0xC7, B: 0x59, A: 0xFF}
	systemOrange = color.NRGBA{R: 0xFF, G: 0x9F, B: 0x0A, A: 0xFF}
	systemRed    = color.NRGBA{R: 0xFF, G: 0x3B, B: 0x30, A: 0xFF}
)

// Font 返回内置字体。
//
// 仍用打包进二进制的字体而不是系统字体：界面全中文，而 SF Pro 不含中文字形，
// 换成系统字体会在 Windows / Linux 上缺字。
func (m MyTheme) Font(s fyne.TextStyle) fyne.Resource {
	return resourceGbkTtf
}

func (*MyTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	dark := v == theme.VariantDark

	switch n {
	case theme.ColorNameBackground:
		if dark {
			return darkBackground
		}
		return lightBackground

	// 输入框与列表底色比窗口底浅一层，macOS 靠这层差别区分「可编辑区」
	case theme.ColorNameInputBackground, theme.ColorNameOverlayBackground,
		theme.ColorNameMenuBackground:
		if dark {
			return darkSurface
		}
		return lightSurface

	case theme.ColorNameForeground:
		if dark {
			return darkForeground
		}
		return lightForeground

	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		if dark {
			return darkSecondary
		}
		return lightSecondary

	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		if dark {
			return darkSeparator
		}
		return lightSeparator

	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		if dark {
			return darkPrimary
		}
		return lightPrimary

	// 按钮用面色而非主色：macOS 里只有主操作是蓝底，其余是白底细边
	case theme.ColorNameButton:
		if dark {
			return darkSurface
		}
		return lightSurface

	case theme.ColorNameHover:
		if dark {
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x14}
		}
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x0D}

	case theme.ColorNameSelection:
		if dark {
			return color.NRGBA{R: 0x29, G: 0x97, B: 0xFF, A: 0x59}
		}
		return color.NRGBA{R: 0x00, G: 0x71, B: 0xE3, A: 0x33}

	case theme.ColorNameSuccess:
		return systemGreen
	case theme.ColorNameWarning:
		return systemOrange
	case theme.ColorNameError:
		return systemRed

	// 默认阴影太重，会让浮层看起来是贴纸
	case theme.ColorNameShadow:
		if dark {
			return color.NRGBA{A: 0x66}
		}
		return color.NRGBA{A: 0x1F}
	}

	return theme.DefaultTheme().Color(n, v)
}

func (*MyTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}

func (*MyTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	// 留白加大一档。Apple 界面的「透气」几乎全来自留白，不是别的
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 10

	// macOS 系统字号是 13pt
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 20
	case theme.SizeNameSubHeadingText:
		return 15
	case theme.SizeNameCaptionText:
		return 11

	// 圆角：控件 8，选中态 6
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 6

	// 细边框，配合上面的分隔线颜色
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameSeparatorThickness:
		return 1

	// 细滚动条，不占内容宽度
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 4
	}

	return theme.DefaultTheme().Size(n)
}

func Mp3() fyne.Resource {
	return resource1Mp3
}

package systemlogic

const (
	LayoutSizeFullwidth = "fullwidth"
	LayoutSizeFull      = "full"
	LayoutSizeTablet    = "tablet"
	LayoutSizeContent   = "content"
	LayoutSizeBoxed     = "boxed"
	LayoutSizeLaptop    = "laptop"
	LayoutSizeMedium    = "medium"
	LayoutSizeMobile    = "mobile"
	LayoutSizeText      = "text"
	LayoutColumns       = 12
)

var LayoutSizes = []string{LayoutSizeFullwidth, LayoutSizeFull, LayoutSizeTablet, LayoutSizeContent, LayoutSizeBoxed, LayoutSizeLaptop, LayoutSizeMedium, LayoutSizeMobile, LayoutSizeText}

func LayoutSize(value, fallback string) string { return buttonOption(value, fallback, LayoutSizes...) }

func LayoutColumnSpan(value, fallback int) int {
	if value < 1 || value > LayoutColumns {
		return fallback
	}
	return value
}

func LayoutContainerHalfWidth(size string) string {
	return map[string]string{
		LayoutSizeBoxed: "46.875rem", LayoutSizeContent: "33.75rem", LayoutSizeTablet: "18.75rem", LayoutSizeLaptop: "42.71875rem", LayoutSizeMedium: "28.75rem", LayoutSizeMobile: "15rem", LayoutSizeText: "22.75rem",
	}[size]
}

package systemlogic

import (
	"net/url"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewLogoGridData struct{ Noop }
type ViewLogoGridItemData struct{ Noop }

func (ViewLogoGridData) Render(_ *Context, props Props) (Data, error) {
	limit := props.Int("mobileLimit")
	if limit < 0 {
		limit = 0
	}
	if _, exists := props["mobileLimit"]; !exists {
		limit = 6
	}
	title := strings.TrimSpace(props.String("title"))
	return Data{
		"logoGridTitle":       title,
		"logoGridHasTitle":    title != "",
		"logoGridVariant":     buttonOption(props.String("variant"), "gray-hover", "gray-hover", "color", "gray"),
		"logoGridMobileLimit": limit,
		"logoGridShowMore":    props.Bool("showMoreCount"),
	}, nil
}

func (ViewLogoGridItemData) Render(_ *Context, props Props) (Data, error) {
	href := strings.TrimSpace(props.String("link"))
	external := false
	if parsed, err := url.Parse(href); err == nil {
		external = parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https")
	}
	return Data{
		"logoGridItemSrc":    strings.TrimSpace(props.String("src")),
		"logoGridItemAlt":    defaultString(strings.TrimSpace(props.String("alt")), "Logo"),
		"logoGridItemHref":   href,
		"logoGridItemIsLink": href != "",
		"logoGridItemIsDiv":  href == "",
		"logoGridItemTarget": map[bool]string{true: "_blank"}[external],
		"logoGridItemRel":    map[bool]string{true: "noopener noreferrer"}[external],
		"logoGridItemDark":   props.Bool("dark"),
	}, nil
}

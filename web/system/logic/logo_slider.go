package systemlogic

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewLogoSliderData struct{ Noop }
type ViewLogoSliderItemData struct{ Noop }

func (ViewLogoSliderData) Render(_ *Context, props Props) (Data, error) {
	speed := 30.0
	if value, exists := props["speed"]; exists && value != nil {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64); err == nil && parsed >= .5 && parsed <= 600 {
			speed = parsed
		}
	}
	return Data{
		"logoSliderClasses":   strings.TrimSpace(strings.Join([]string{props.String("class"), props.String("containerClass")}, " ")),
		"logoSliderStyle":     fmt.Sprintf("--view-logo-slider-duration:%gs", speed),
		"logoSliderGrayscale": defaultBool(props, "grayscale", true),
		"logoSliderLabel":     defaultString(strings.TrimSpace(props.String("ariaLabel")), "Logo carousel"),
	}, nil
}

func (ViewLogoSliderItemData) Render(_ *Context, props Props) (Data, error) {
	href := strings.TrimSpace(props.String("href"))
	external := false
	if parsed, err := url.Parse(href); err == nil {
		external = parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https")
	}
	return Data{
		"logoSliderItemSrc":    strings.TrimSpace(props.String("src")),
		"logoSliderItemAlt":    strings.TrimSpace(props.String("alt")),
		"logoSliderItemHref":   href,
		"logoSliderItemIsLink": href != "",
		"logoSliderItemIsDiv":  href == "",
		"logoSliderItemTarget": map[bool]string{true: "_blank"}[external],
		"logoSliderItemRel":    map[bool]string{true: "noopener noreferrer"}[external],
	}, nil
}

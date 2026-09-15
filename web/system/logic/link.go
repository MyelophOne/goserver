package systemlogic

import (
	"fmt"
	"net/url"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type UiLinkData struct{ Noop }

func (UiLinkData) Render(_ *Context, props Props) (Data, error) {
	href := strings.TrimSpace(props.String("href"))
	if href == "" {
		href = strings.TrimSpace(props.String("to"))
	}
	external := props.Bool("external") || linkIsExternal(href)
	target := strings.TrimSpace(props.String("target"))
	disabled := props.Bool("disabled")
	return Data{
		"uiLinkHref":         href,
		"uiLinkTarget":       target,
		"uiLinkRel":          cardRel(target, strings.TrimSpace(props.String("rel"))),
		"uiLinkExternal":     external,
		"uiLinkDisabled":     disabled,
		"uiLinkAriaDisabled": fmt.Sprintf("%t", disabled),
		"uiLinkAriaLabel":    strings.TrimSpace(props.String("ariaLabel")),
		"uiLinkTitle":        strings.TrimSpace(props.String("title")),
	}, nil
}

func linkIsExternal(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.IsAbs() || parsed.Scheme == "mailto" || parsed.Scheme == "tel"
}

package systemlogic

import (
	"fmt"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type PageFullscreenPreloaderData struct{ Noop }

func (PageFullscreenPreloaderData) Render(_ *Context, props Props) (Data, error) {
	background := safePreloaderBackground(props.String("background"), "var(--ui-bg)")
	backgroundDark := safePreloaderBackground(props.String("backgroundDark"), background)
	zIndex := props.Int("zIndex")
	if zIndex <= 0 {
		zIndex = 9999
	}
	minimumDuration := props.Int("minimumDuration")
	if minimumDuration < 0 || minimumDuration > 10000 {
		minimumDuration = 250
	}
	if _, exists := props["minimumDuration"]; !exists {
		minimumDuration = 250
	}
	transparent := props.Bool("transparent")
	if transparent {
		background, backgroundDark = "transparent", "transparent"
	}
	return Data{
		"pagePreloaderStyle":    fmt.Sprintf("--page-fullscreen-preloader-background-light:%s;--page-fullscreen-preloader-background-dark:%s;--page-fullscreen-preloader-z-index:%d", background, backgroundDark, zIndex),
		"pagePreloaderClasses":  strings.TrimSpace("is-visible " + props.String("class")),
		"pagePreloaderLabel":    defaultString(strings.TrimSpace(props.String("ariaLabel")), "Page loading"),
		"pagePreloaderDuration": minimumDuration,
	}, nil
}

func safePreloaderBackground(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ";{}<>\\") {
		return fallback
	}
	return value
}

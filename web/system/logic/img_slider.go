package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewImgSliderData struct{ Noop }

type ViewImgSliderImageData struct{ Noop }
type ViewImgSliderVideoData struct{ Noop }

func (ViewImgSliderData) Render(_ *Context, props Props) (Data, error) {
	interval := props.Int("autoScrollInterval")
	if interval < 1000 || interval > 60000 {
		interval = 4000
	}
	return Data{
		"imgSliderAutoScroll": defaultBool(props, "autoScroll", true),
		"imgSliderInterval":   interval,
		"imgSliderVertical":   props.Bool("vertical"),
		"imgSliderLabel":      defaultString(strings.TrimSpace(props.String("ariaLabel")), "Image slider"),
		"imgSliderPrevious":   defaultString(strings.TrimSpace(props.String("previousLabel")), "Previous slide"),
		"imgSliderNext":       defaultString(strings.TrimSpace(props.String("nextLabel")), "Next slide"),
	}, nil
}

func (ViewImgSliderImageData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"imgSliderImageSrc":     strings.TrimSpace(props.String("src")),
		"imgSliderImageAlt":     strings.TrimSpace(props.String("alt")),
		"imgSliderImageLoading": buttonOption(props.String("loading"), "lazy", "lazy", "eager"),
	}, nil
}

func (ViewImgSliderVideoData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"imgSliderVideoSrc":    strings.TrimSpace(props.String("src")),
		"imgSliderVideoPoster": strings.TrimSpace(props.String("poster")),
		"imgSliderVideoLabel":  defaultString(strings.TrimSpace(props.String("label")), "Video slide"),
	}, nil
}

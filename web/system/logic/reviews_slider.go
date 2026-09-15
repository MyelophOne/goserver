package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewReviewsSliderData struct{ Noop }
type ViewReviewSlideData struct{ Noop }

func (ViewReviewsSliderData) Render(_ *Context, props Props) (Data, error) {
	interval := props.Int("autoScrollInterval")
	if interval < 1000 || interval > 60000 {
		interval = 4000
	}
	return Data{"reviewsAutoScroll": defaultBool(props, "autoScroll", true), "reviewsInterval": interval, "reviewsLabel": defaultString(strings.TrimSpace(props.String("ariaLabel")), "Customer reviews")}, nil
}

func (ViewReviewSlideData) Render(_ *Context, props Props) (Data, error) {
	name, city, avatar, link := strings.TrimSpace(props.String("name")), strings.TrimSpace(props.String("city")), strings.TrimSpace(props.String("avatar")), strings.TrimSpace(props.String("link"))
	return Data{
		"reviewText":        strings.TrimSpace(props.String("text")),
		"reviewName":        name,
		"reviewCity":        city,
		"reviewAvatar":      avatar,
		"reviewLink":        link,
		"reviewLinkLabel":   defaultString(strings.TrimSpace(props.String("linkLabel")), "Подробнее"),
		"reviewHasFooter":   name != "" || link != "" || avatar != "",
		"reviewHasName":     name != "",
		"reviewHasCity":     city != "",
		"reviewHasAvatar":   avatar != "",
		"reviewHasIdentity": name != "" || city != "",
		"reviewHasLink":     link != "",
	}, nil
}

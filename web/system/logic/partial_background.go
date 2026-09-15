package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewPartialBackgroundData struct{ Noop }

func (ViewPartialBackgroundData) Render(_ *Context, props Props) (Data, error) {
	background := partialBackgroundValue(props.String("background"), "transparent")
	backgroundColor := partialBackgroundValue(props.String("backgroundColor"), "transparent")
	style := strings.Join([]string{
		"--partial-background-left:" + partialBackgroundValue(props.String("backgroundHeightLeft"), "60dvh"),
		"--partial-background-right:" + partialBackgroundValue(props.String("backgroundHeightRight"), "60dvh"),
		"--partial-background-before-content-height:" + partialBackgroundValue(props.String("backgroundBeforeContentHeight"), "clamp(5rem, 12dvh, 10rem)"),
		"--partial-background-after-content-height:" + partialBackgroundValue(props.String("backgroundAfterContentHeight"), "clamp(5rem, 12dvh, 10rem)"),
		"--partial-background-min-height:" + partialBackgroundValue(props.String("minHeight"), "auto"),
		"--partial-background-color-light:" + backgroundColor,
		"--partial-background-color-dark:" + partialBackgroundValue(props.String("backgroundColorDark"), backgroundColor),
		"--partial-background-light:" + background,
		"--partial-background-dark:" + partialBackgroundValue(props.String("backgroundDark"), background),
		"--partial-background-size:" + partialBackgroundValue(props.String("backgroundSize"), "cover"),
		"--partial-background-position:" + partialBackgroundValue(props.String("backgroundPosition"), "center"),
		"--partial-background-repeat:" + partialBackgroundValue(props.String("backgroundRepeat"), "no-repeat"),
		"--partial-background-mask-light:" + partialBackgroundValue(props.String("mask"), "none"),
		"--partial-background-mask-dark:" + partialBackgroundValue(props.String("maskDark"), partialBackgroundValue(props.String("mask"), "none")),
		"--partial-background-mask-size:" + partialBackgroundValue(props.String("maskSize"), "cover"),
		"--partial-background-mask-position:" + partialBackgroundValue(props.String("maskPosition"), "center"),
		"--partial-background-mask-repeat:" + partialBackgroundValue(props.String("maskRepeat"), "no-repeat"),
	}, ";")
	as := buttonOption(props.String("as"), "section", "section", "div", "article")
	return Data{
		"partialBackgroundStyle":          style,
		"partialBackgroundContainerSize":  LayoutSize(props.String("containerSize"), LayoutSizeContent),
		"partialBackgroundBefore":         props.Bool("backgroundBeforeContent"),
		"partialBackgroundAfter":          props.Bool("backgroundAfterContent"),
		"partialBackgroundAlign":          buttonOption(props.String("contentAlign"), "top", "top", "center", "bottom"),
		"partialBackgroundRootClasses":    strings.TrimSpace(props.String("class")),
		"partialBackgroundContentClasses": strings.TrimSpace(props.String("contentClass")),
		"partialBackgroundLayerClasses":   strings.TrimSpace(props.String("layerClass")),
		"partialBackgroundIsSection":      as == "section",
		"partialBackgroundIsDiv":          as == "div",
		"partialBackgroundIsArticle":      as == "article",
	}, nil
}

func partialBackgroundValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ";{}<>\\") {
		return fallback
	}
	return value
}

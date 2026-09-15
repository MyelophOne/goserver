package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewSocialBarData struct{ Noop }
type ViewSocialBarItemData struct{ Noop }

func (ViewSocialBarData) Render(_ *Context, props Props) (Data, error) {
	size := normalizeIconSize(strings.TrimSpace(props.String("size")))
	if size == "" {
		size = "24px"
	}
	return Data{
		"socialBarClasses":   strings.TrimSpace(props.String("class")),
		"socialBarStyle":     "--view-social-bar-size:" + size + ";--view-social-bar-gap:" + socialBarGap(props.String("gap")),
		"socialBarVariant":   buttonOption(props.String("variant"), "simple", "simple", "colored", "circle", "solid", "glass"),
		"socialBarIconClass": strings.TrimSpace(props.String("iconClass")),
		"socialBarLabel":     defaultString(strings.TrimSpace(props.String("ariaLabel")), "Social links"),
	}, nil
}

func (ViewSocialBarItemData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"socialItemName":       strings.TrimSpace(props.String("name")),
		"socialItemTo":         strings.TrimSpace(props.String("to")),
		"socialItemLabel":      defaultString(strings.TrimSpace(props.String("label")), "Social link"),
		"socialItemColorClass": strings.TrimSpace(props.String("color")),
		"socialItemTarget":     defaultString(strings.TrimSpace(props.String("target")), "_blank"),
	}, nil
}

func socialBarGap(value string) string {
	value = strings.TrimSpace(value)
	if gap, ok := map[string]string{"gap-1": ".25rem", "gap-2": ".5rem", "gap-3": ".75rem", "gap-4": "1rem", "gap-5": "1.25rem", "gap-6": "1.5rem", "gap-8": "2rem", "gap-10": "2.5rem"}[value]; ok {
		return gap
	}
	return safeCSSSize(value, "1rem")
}

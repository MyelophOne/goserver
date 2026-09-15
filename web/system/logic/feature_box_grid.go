package systemlogic

import (
	"fmt"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewFeatureBoxGridData struct{ Noop }

type ViewFeatureBoxData struct{ Noop }

func (ViewFeatureBoxGridData) Render(_ *Context, props Props) (Data, error) {
	columns := props.Int("columns")
	if columns < 2 || columns > 4 {
		columns = 3
	}
	iconSize := props.Int("iconSize")
	if iconSize < 12 || iconSize > 128 {
		iconSize = 42
	}
	return Data{
		"featureGridStyle":     fmt.Sprintf("--view-feature-box-columns:%d;--view-feature-box-icon-size:%dpx", columns, iconSize),
		"featureGridVertical":  props.Bool("vertical"),
		"featureGridDescAlign": buttonOption(props.String("descAlign"), "left", "left", "center", "right", "justify"),
		"featureGridIconClass": strings.TrimSpace(props.String("defaultIconClass")),
	}, nil
}

func (ViewFeatureBoxData) Render(_ *Context, props Props) (Data, error) {
	href := strings.TrimSpace(props.String("to"))
	icon, title, description := strings.TrimSpace(props.String("icon")), strings.TrimSpace(props.String("title")), strings.TrimSpace(props.String("description"))
	return Data{
		"featureBoxHref":           href,
		"featureBoxIsLink":         href != "",
		"featureBoxIsDiv":          href == "",
		"featureBoxIcon":           icon,
		"featureBoxTitle":          title,
		"featureBoxSubtitle":       strings.TrimSpace(props.String("subtitle")),
		"featureBoxDescription":    description,
		"featureBoxHasIcon":        icon != "",
		"featureBoxHasTitle":       title != "",
		"featureBoxHasHeading":     icon != "" || title != "",
		"featureBoxHasSubtitle":    strings.TrimSpace(props.String("subtitle")) != "",
		"featureBoxHasDescription": description != "",
		"featureBoxIconClass":      strings.TrimSpace(props.String("iconClass")),
	}, nil
}

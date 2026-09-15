package systemlogic

import (
	"fmt"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewInfoBarData struct{ Noop }
type ViewInfoBarItemData struct{ Noop }
type ViewInfoBarActionData struct{ Noop }

func (ViewInfoBarData) Render(_ *Context, props Props) (Data, error) {
	iconSize := normalizeIconSize(strings.TrimSpace(props.String("iconSize")))
	if iconSize == "" {
		iconSize = "1rem"
	}
	return Data{"infoBarClasses": strings.TrimSpace(strings.Join([]string{props.String("class"), props.String("wrapperClass")}, " ")), "infoBarIconSize": iconSize, "infoBarLabel": defaultString(strings.TrimSpace(props.String("ariaLabel")), "Information bar")}, nil
}

func (ViewInfoBarItemData) Render(_ *Context, props Props) (Data, error) {
	label, exists := props["label"]
	return Data{
		"infoBarItemIcon":        strings.TrimSpace(props.String("icon")),
		"infoBarItemLabel":       strings.TrimSpace(fmt.Sprint(label)),
		"infoBarItemHasLabel":    exists && label != nil,
		"infoBarItemIconClass":   strings.TrimSpace(props.String("iconClass")),
		"infoBarItemTextClass":   strings.TrimSpace(props.String("textClass")),
		"infoBarItemInteractive": props.Bool("interactive"),
	}, nil
}

func (ViewInfoBarActionData) Render(_ *Context, props Props) (Data, error) {
	label := strings.TrimSpace(props.String("label"))
	return Data{"infoBarActionIcon": strings.TrimSpace(props.String("icon")), "infoBarActionLabel": label, "infoBarActionIconClass": strings.TrimSpace(props.String("iconClass")), "infoBarActionTitle": defaultString(label, "Action")}, nil
}

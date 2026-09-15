package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewAnnouncementBarData struct{ Noop }

func (ViewAnnouncementBarData) Render(_ *Context, props Props) (Data, error) {
	title := strings.TrimSpace(props.String("title"))
	return Data{
		"announcementTitle":       title,
		"announcementDescription": strings.TrimSpace(props.String("description")),
		"announcementIcon":        strings.TrimSpace(props.String("icon")),
		"announcementHasTitle":    title != "",
		"announcementColor":       buttonOption(props.String("color"), "primary", "gray", "primary", "info", "success", "warning", "error"),
		"announcementVariant":     buttonOption(props.String("variant"), "soft", "solid", "soft", "outline"),
		"announcementSticky":      props.Bool("sticky"),
		"announcementClosable":    props.Bool("closable"),
		"announcementCloseLabel":  defaultString(strings.TrimSpace(props.String("closeLabel")), "Close announcement"),
	}, nil
}

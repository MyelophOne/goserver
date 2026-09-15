package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewAvatarGroupData struct{ Noop }

func (ViewAvatarGroupData) Render(_ *Context, props Props) (Data, error) {
	max := props.Int("max")
	if _, exists := props["max"]; !exists {
		max = 7
	}
	if max < 0 {
		max = 0
	}
	return Data{
		"avatarGroupSize":  buttonOption(props.String("size"), "md", "sm", "md", "lg", "xl"),
		"avatarGroupMax":   max,
		"avatarGroupLabel": defaultString(strings.TrimSpace(props.String("ariaLabel")), "Avatar group"),
	}, nil
}

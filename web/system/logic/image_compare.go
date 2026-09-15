package systemlogic

import (
	"fmt"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewImageCompareData struct{ Noop }

func (ViewImageCompareData) Render(_ *Context, props Props) (Data, error) {
	position := props.Int("position")
	if position < 0 || position > 100 {
		position = 50
	}
	if _, exists := props["position"]; !exists {
		position = 50
	}
	height := safeCSSSize(props.String("height"), "28rem")
	beforeLabel := defaultString(strings.TrimSpace(props.String("beforeLabel")), "Before")
	afterLabel := defaultString(strings.TrimSpace(props.String("afterLabel")), "After")
	return Data{
		"compareBeforeImage": strings.TrimSpace(props.String("beforeImage")),
		"compareAfterImage":  strings.TrimSpace(props.String("afterImage")),
		"compareBeforeAlt":   strings.TrimSpace(props.String("beforeAlt")),
		"compareAfterAlt":    strings.TrimSpace(props.String("afterAlt")),
		"compareBeforeLabel": beforeLabel,
		"compareAfterLabel":  afterLabel,
		"comparePosition":    position,
		"compareStyle":       fmt.Sprintf("--view-image-compare-position:%d%%;--view-image-compare-height:%s", position, height),
		"compareAriaLabel":   defaultString(strings.TrimSpace(props.String("ariaLabel")), beforeLabel+" and "+afterLabel+" comparison"),
	}, nil
}

package systemlogic

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewInfiniteMarqueeData struct{ Noop }

func (ViewInfiniteMarqueeData) Render(_ *Context, props Props) (Data, error) {
	duration := marqueeNumber(props, "duration", 30, .5, 600)
	delay := props.Int("delay")
	if delay < 0 || delay > 10000 {
		delay = 1000
	}
	if _, exists := props["delay"]; !exists {
		delay = 1000
	}
	classes := strings.TrimSpace(strings.Join([]string{props.String("class"), props.String("containerClass")}, " "))
	return Data{
		"marqueeClasses":   classes,
		"marqueeStyle":     fmt.Sprintf("--view-marquee-duration:%gs", duration),
		"marqueeDelay":     delay,
		"marqueeAriaLabel": defaultString(strings.TrimSpace(props.String("ariaLabel")), "Scrolling content"),
	}, nil
}

func marqueeNumber(props Props, name string, fallback, min, max float64) float64 {
	value, exists := props[name]
	if !exists || value == nil {
		return fallback
	}
	result, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	if err != nil || result < min || result > max {
		return fallback
	}
	return result
}

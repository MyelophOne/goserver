package systemlogic

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewAnimatedCounterData struct{ Noop }

func (ViewAnimatedCounterData) Render(_ *Context, props Props) (Data, error) {
	to := counterNumber(props, "to", 0)
	from := counterNumber(props, "from", 0)
	decimals := props.Int("decimals")
	if decimals < 0 || decimals > 10 {
		decimals = 0
	}
	duration := props.Int("duration")
	if duration <= 0 || duration > 60000 {
		duration = 1500
	}
	separator := props.String("separator")
	if separator == "" {
		separator = " "
	}
	prefix, suffix := props.String("prefix"), props.String("suffix")
	return Data{
		"counterTo":               strconv.FormatFloat(to, 'f', decimals, 64),
		"counterFrom":             strconv.FormatFloat(from, 'f', decimals, 64),
		"counterDuration":         duration,
		"counterDecimals":         decimals,
		"counterSeparator":        separator,
		"counterPrefix":           prefix,
		"counterSuffix":           suffix,
		"counterOnce":             defaultBool(props, "once", true),
		"counterTargetFormatted":  formatCounter(to, decimals, separator),
		"counterInitialFormatted": formatCounter(from, decimals, separator),
		"counterAriaLabel":        fmt.Sprintf("%s%s%s", prefix, formatCounter(to, decimals, separator), suffix),
	}, nil
}

func counterNumber(props Props, name string, fallback float64) float64 {
	value, exists := props[name]
	if !exists || value == nil {
		return fallback
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return fallback
	}
	return number
}

func formatCounter(value float64, decimals int, separator string) string {
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	parts := strings.SplitN(text, ".", 2)
	integer := parts[0]
	sign := ""
	if strings.HasPrefix(integer, "-") {
		sign, integer = "-", strings.TrimPrefix(integer, "-")
	}
	for index := len(integer) - 3; index > 0; index -= 3 {
		integer = integer[:index] + separator + integer[index:]
	}
	if len(parts) == 2 {
		return sign + integer + "." + parts[1]
	}
	return sign + integer
}

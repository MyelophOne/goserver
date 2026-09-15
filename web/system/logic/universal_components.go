package systemlogic

import (
	"strings"
	"unicode"

	. "github.com/myelophone/goserver/web/runtime"
)

type UniversalComponentData struct{ Noop }

func (UniversalComponentData) Render(_ *Context, props Props) (Data, error) {
	data := Data{}
	for key, value := range props {
		data[key] = value
		data[camelComponentProp(key)] = value
	}
	return data, nil
}

func camelComponentProp(value string) string {
	var out strings.Builder
	upper := false
	for _, r := range value {
		if r == '-' || r == '_' {
			upper = true
			continue
		}
		if upper {
			out.WriteRune(unicode.ToUpper(r))
			upper = false
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

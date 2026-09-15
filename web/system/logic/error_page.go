package systemlogic

import (
	"strconv"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type NotFoundPageData struct{ Noop }

func (NotFoundPageData) Render(ctx *Context, _ Props) (Data, error) {
	return errorPageData(ctx, 404), nil
}

type ErrorPageData struct{ Noop }

func (ErrorPageData) Render(ctx *Context, props Props) (Data, error) {
	statusCode := props.Int("statusCode")
	if statusCode < 400 || statusCode > 599 {
		statusCode = 500
	}
	return errorPageData(ctx, statusCode), nil
}

func errorPageData(ctx *Context, statusCode int) Data {
	prefix := "common.errors.server"
	titleFallback := "Something went wrong"
	descriptionFallback := "The server could not complete this request. Your data has not been changed; please try again shortly."
	if statusCode == 404 {
		prefix = "common.errors.notFound"
		titleFallback = "Page not found"
		descriptionFallback = "The address may be incorrect, the page may have moved, or it may no longer be available."
	}
	title := errorPageText(ctx, prefix+".title", titleFallback)
	description := errorPageText(ctx, prefix+".description", descriptionFallback)
	UseSeo(ctx, SEOInput{
		Title:       title + " · " + strconv.Itoa(statusCode),
		Description: description,
		NoIndex:     SEOFlag(true),
	})
	return Data{
		"errorCode":        strconv.Itoa(statusCode),
		"errorTitle":       title,
		"errorDescription": description,
		"errorHome":        errorPageText(ctx, "common.errors.home", "Back to home"),
	}
}

func errorPageText(ctx *Context, key, fallback string) string {
	if ctx == nil {
		return fallback
	}
	value := strings.TrimSpace(ctx.T(key))
	if value == "" || value == key {
		return fallback
	}
	return value
}

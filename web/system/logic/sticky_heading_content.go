package systemlogic

import (
	"fmt"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewStickyHeadingContentData struct{ Noop }

func (ViewStickyHeadingContentData) Render(_ *Context, props Props) (Data, error) {
	as := buttonOption(props.String("as"), "section", "section", "div", "article")
	stickyOffset := safeCSSSize(props.String("stickyOffset"), "6rem")
	return Data{
		"stickyContentIsSection":          as == "section",
		"stickyContentIsDiv":              as == "div",
		"stickyContentIsArticle":          as == "article",
		"stickyContentTitle":              strings.TrimSpace(props.String("title")),
		"stickyContentHeadingLevel":       LayoutColumnSpan(props.Int("headingLevel"), 2),
		"stickyContentHeadingSize":        buttonOption(props.String("headingSize"), "lg", "xs", "sm", "md", "lg", "xl", "display"),
		"stickyContentHeadingSpan":        LayoutColumnSpan(props.Int("headingSpan"), 5),
		"stickyContentBodySpan":           LayoutColumnSpan(props.Int("contentSpan"), 7),
		"stickyContentContainerSize":      LayoutSize(props.String("containerSize"), LayoutSizeContent),
		"stickyContentSide":               buttonOption(props.String("headingSide"), "left", "left", "right"),
		"stickyContentSticky":             defaultBool(props, "sticky", true),
		"stickyContentFrom":               buttonOption(props.String("stickyFrom"), "lg", "always", "sm", "md", "lg"),
		"stickyContentStyle":              "--view-sticky-heading-offset:" + stickyOffset,
		"stickyContentRootClass":          strings.TrimSpace(props.String("class")),
		"stickyContentContainerClass":     strings.TrimSpace(props.String("containerClass")),
		"stickyContentRowClass":           strings.TrimSpace(props.String("rowClass")),
		"stickyContentHeadingColumnClass": strings.TrimSpace(props.String("headingColumnClass")),
		"stickyContentHeadingClass":       strings.TrimSpace(props.String("headingClass")),
		"stickyContentBodyColumnClass":    strings.TrimSpace(props.String("contentColumnClass")),
		"stickyContentBodyClass":          strings.TrimSpace(props.String("contentClass")),
		"stickyContentHasTitle":           strings.TrimSpace(props.String("title")) != "",
		"stickyContentLabel":              defaultString(strings.TrimSpace(props.String("ariaLabel")), "Sticky heading content"),
		"stickyContentSpanStyle":          fmt.Sprintf("--view-sticky-heading-span:%d;--view-sticky-content-span:%d", LayoutColumnSpan(props.Int("headingSpan"), 5), LayoutColumnSpan(props.Int("contentSpan"), 7)),
	}, nil
}

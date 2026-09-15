package systemlogic

import (
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type ViewQuoteBigData struct{ Noop }

func (ViewQuoteBigData) Render(_ *Context, props Props) (Data, error) {
	authorName := strings.TrimSpace(props.String("authorName"))
	authorRole := strings.TrimSpace(props.String("authorRole"))
	avatarSrc := strings.TrimSpace(props.String("avatarSrc"))
	return Data{
		"quoteBigText":          strings.TrimSpace(props.String("text")),
		"quoteBigAuthorName":    authorName,
		"quoteBigAuthorRole":    authorRole,
		"quoteBigAvatarSrc":     avatarSrc,
		"quoteBigAvatarChip":    strings.TrimSpace(props.String("avatarChipColor")),
		"quoteBigHasFooter":     authorName != "" || authorRole != "" || avatarSrc != "",
		"quoteBigHasAuthorName": authorName != "",
		"quoteBigHasAuthorRole": authorRole != "",
		"quoteBigHasAvatar":     avatarSrc != "",
		"quoteBigTextClasses":   strings.TrimSpace(props.String("textSizeClass")),
		"quoteBigQuoteAlign":    buttonOption(props.String("quoteAlign"), "center", "left", "center", "right"),
		"quoteBigFooterAlign":   buttonOption(props.String("footerAlign"), "center", "left", "center", "right"),
	}, nil
}

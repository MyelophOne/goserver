package systemlogic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type UiIconData struct{ Noop }

type UiButtonData struct{ Noop }

type UiInputData struct{ Noop }

type UiTextareaData struct{ Noop }

type UiCheckboxData struct{ Noop }

type UiTogglerData struct{ Noop }

type UiViewportSpacerData struct{ Noop }

type UiFileUploadData struct{ Noop }

type UiAlertData struct{ Noop }

type UiAvatarData struct{ Noop }

type UiBadgeData struct{ Noop }

type UiBannerData struct{ Noop }

type UiTruncateTextData struct{ Noop }

type UiTooltipData struct{ Noop }

type UiTabsData struct{ Noop }

type UiTableData struct{ Noop }

type UiStickyWrapperData struct{ Noop }

type UiSplitSectionData struct{ Noop }

type UiSmartContrastData struct{ Noop }

type UiSnapContainerData struct{ Noop }

type UiSnapSectionData struct{ Noop }

type UiSegmentedControlData struct{ Noop }

type UiSegmentedControlItemData struct{ Noop }

type UiCardData struct{ Noop }

type UiChipData struct{ Noop }

type UiTextColumnData struct{ Noop }

type UiHeadingData struct{ Noop }

type UiProtectedEmailData struct{ Noop }

type UiStepsData struct{ Noop }

type UiStepData struct{ Noop }

type UiScrollToTopData struct{ Noop }

var iconSizePattern = regexp.MustCompile(`^\d+(?:\.\d+)?(?:px|rem|em|vh|vw|%|pt|pc|in|cm|mm)?$`)
var iconPixelSizePattern = regexp.MustCompile(`^\d+$`)
var viewportSpacerSizePattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)`)

func (UiIconData) Render(_ *Context, props Props) (Data, error) {
	name := strings.TrimSpace(props.String("name"))
	iconURL := ""
	if parts := strings.SplitN(name, ":", 2); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		iconURL = "https://api.iconify.design/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + ".svg"
	}

	size := normalizeIconSize(props.String("size"))
	iconKey := ""
	if iconURL != "" {
		sum := sha256.Sum256([]byte(iconURL + "\x00" + size))
		iconKey = "ui-icon-" + hex.EncodeToString(sum[:6])
	}

	icon8Size := props.String("size")
	if icon8Size == "" {
		icon8Size = "28"
	}
	icon8Type := props.String("type")
	if icon8Type == "" {
		icon8Type = "ios-filled"
	}
	color := strings.TrimPrefix(strings.TrimSpace(props.String("color")), "#")
	if color == "" {
		color = "000000"
	}
	icon8 := strings.TrimSpace(props.String("icon"))
	icon8URL := ""
	if icon8 != "" {
		icon8URL = "https://img.icons8.com/" + url.PathEscape(icon8Type) + "/" + url.PathEscape(icon8Size) + "/" + url.PathEscape(color) + "/" + url.PathEscape(icon8) + ".png"
	}

	return Data{"iconUrl": iconURL, "iconSize": size, "iconKey": iconKey, "icon8Url": icon8URL, "icon8Size": icon8Size, "icon8Alt": icon8, "iconName": name}, nil
}

func (UiButtonData) Render(_ *Context, props Props) (Data, error) {
	as := strings.ToLower(strings.TrimSpace(props.String("as")))
	href := strings.TrimSpace(props.String("href"))
	if href == "" {
		href = strings.TrimSpace(props.String("to"))
	}
	isLink := as == "a" || as == "nuxt-link" || (as == "" && href != "")
	loading, disabled := props.Bool("loading"), props.Bool("disabled")
	leading := strings.TrimSpace(props.String("leadingIcon"))
	if leading == "" {
		leading = strings.TrimSpace(props.String("icon"))
	}
	return Data{
		"isLink": isLink, "isButton": !isLink, "linkHref": href, "buttonType": defaultString(props.String("type"), "button"),
		"buttonColor":   buttonOption(props.String("color"), "base", "base", "gray", "primary", "info", "success", "warning", "error"),
		"buttonVariant": buttonOption(props.String("variant"), "solid", "solid", "soft", "outline", "ghost"),
		"buttonSize":    buttonOption(props.String("size"), "md", "xs", "sm", "md", "lg"),
		"isDisabled":    disabled || loading, "isLoading": loading, "leadingIconName": leading, "hasLeadingIcon": leading != "" && !loading,
		"trailingIconName": strings.TrimSpace(props.String("trailingIcon")), "hasTrailingIcon": strings.TrimSpace(props.String("trailingIcon")) != "" && !loading,
		"isBlock": props.Bool("block"), "isRounded": defaultBool(props, "rounded", true), "isSquare": props.Bool("square"),
		"buttonLabel": props.String("label"), "buttonTarget": props.String("target"),
	}, nil
}

func (UiInputData) Render(_ *Context, props Props) (Data, error) {
	value := imagePropString(props, "modelValue")
	if value == "" {
		value = imagePropString(props, "value")
	}
	placeholder := imagePropString(props, "placeholder")
	if placeholder == "" {
		placeholder = " "
	}
	position := strings.ToLower(imagePropString(props, "iconPosition"))
	if position != "start" && position != "end" {
		position = "end"
	}
	return Data{
		"inputValue":        value,
		"inputType":         defaultString(imagePropString(props, "type"), "text"),
		"inputPlaceholder":  placeholder,
		"inputIconPosition": position,
	}, nil
}

func (UiTextareaData) Render(_ *Context, props Props) (Data, error) {
	value := imagePropString(props, "modelValue")
	if value == "" {
		value = imagePropString(props, "value")
	}
	textareaID := defaultString(props.String("id"), props.String("name"))
	position := buttonOption(props.String("buttonsPosition"), "none", "none", "inside", "outside")
	return Data{
		"textareaID":          textareaID,
		"hasLabel":            strings.TrimSpace(props.String("label")) != "" && textareaID != "",
		"textareaValue":       value,
		"textareaRows":        maxInt(props.Int("rows"), 4),
		"textareaPlaceholder": props.String("placeholder"),
		"buttonsInside":       position == "inside",
		"buttonsOutside":      position == "outside",
		"buttonsNotInside":    position != "inside",
		"showClearButton":     position != "none" && defaultBool(props, "showClear", true),
		"showSaveButton":      position != "none" && defaultBool(props, "showSave", true),
		"clearButtonText":     defaultString(props.String("clearText"), "Clear"),
		"saveButtonText":      defaultString(props.String("saveText"), "Save"),
		"hasError":            props.Bool("error"),
	}, nil
}

func (UiCheckboxData) Render(_ *Context, props Props) (Data, error) {
	value := imagePropString(props, "modelValue")
	if value == "" {
		value = imagePropString(props, "value")
	}
	if value == "" {
		value = "on"
	}
	name := strings.TrimSpace(props.String("name"))
	id := strings.TrimSpace(props.String("id"))
	if id == "" {
		id = checkboxID(name, value)
	}
	label := strings.TrimSpace(props.String("label"))
	description := strings.TrimSpace(props.String("description"))
	return Data{
		"checkboxID":          id,
		"checkboxName":        name,
		"checkboxValue":       value,
		"checkboxLabel":       label,
		"checkboxDescription": description,
		"hasCheckboxLabel":    label != "",
		"hasDescription":      description != "",
		"checkboxChecked":     props.Bool("checked"),
		"checkboxDisabled":    props.Bool("disabled"),
		"checkboxRequired":    props.Bool("required"),
	}, nil
}

func (UiTogglerData) Render(_ *Context, props Props) (Data, error) {
	checked := defaultBool(props, "modelValue", props.Bool("checked"))
	return Data{
		"togglerLabel":       defaultString(strings.TrimSpace(props.String("label")), "Переключить"),
		"togglerChecked":     checked,
		"togglerAriaChecked": fmt.Sprintf("%t", checked),
		"togglerDisabled":    props.Bool("disabled"),
	}, nil
}

func (UiViewportSpacerData) Render(_ *Context, props Props) (Data, error) {
	horizontal := props.Bool("horizontal")
	desktop := viewportSpacerSize(props.String("size"), "5")
	mobile := viewportSpacerSize(props.String("mobileSize"), desktop)
	unit, dynamicUnit := "vh", "dvh"
	if horizontal {
		unit, dynamicUnit = "vw", "dvw"
	}
	return Data{
		"spacerHorizontal": horizontal,
		"spacerStyle":      fmt.Sprintf("--sp-desktop:%s%s;--sp-desktop-d:%s%s;--sp-mobile:%s%s;--sp-mobile-d:%s%s", desktop, unit, desktop, dynamicUnit, mobile, unit, mobile, dynamicUnit),
	}, nil
}

func (UiFileUploadData) Render(_ *Context, props Props) (Data, error) {
	name := strings.TrimSpace(props.String("name"))
	id := defaultString(strings.TrimSpace(props.String("id")), "ui-file-upload-"+defaultString(name, "file"))
	errorMessage := ""
	if value, ok := props["error"].(string); ok && !strings.EqualFold(strings.TrimSpace(value), "false") {
		errorMessage = strings.TrimSpace(value)
	}
	return Data{
		"fileUploadID":           id,
		"fileUploadLabel":        defaultString(strings.TrimSpace(props.String("label")), "Upload files"),
		"fileUploadDescription":  strings.TrimSpace(props.String("description")),
		"fileUploadSelectText":   defaultString(strings.TrimSpace(props.String("selectText")), "Select files"),
		"fileUploadEmptyText":    defaultString(strings.TrimSpace(props.String("emptyText")), "or drag and drop them here"),
		"fileUploadLayout":       buttonOption(props.String("layout"), "list", "list", "grid"),
		"fileUploadMultiple":     props.Bool("multiple"),
		"fileUploadRequired":     props.Bool("required"),
		"fileUploadDisabled":     props.Bool("disabled"),
		"fileUploadDropzone":     defaultBool(props, "dropzone", true),
		"fileUploadInteractive":  defaultBool(props, "interactive", true),
		"fileUploadPreview":      defaultBool(props, "preview", true),
		"fileUploadAppend":       defaultBool(props, "append", true),
		"fileUploadError":        props.Bool("error") || errorMessage != "",
		"fileUploadErrorMessage": errorMessage,
		"fileUploadRole":         fileUploadRole(defaultBool(props, "interactive", true), props.Bool("disabled")),
		"fileUploadTabindex":     fileUploadTabindex(defaultBool(props, "interactive", true), props.Bool("disabled")),
	}, nil
}

func (UiAlertData) Render(_ *Context, props Props) (Data, error) {
	description := strings.TrimSpace(props.String("description"))
	avatarSrc := strings.TrimSpace(props.String("avatarSrc"))
	avatarIcon := strings.TrimSpace(props.String("avatarIcon"))
	hasAvatar := avatarSrc != "" || avatarIcon != ""
	avatarSize := buttonOption(props.String("avatarSize"), "", "", "sm", "md", "lg")
	if avatarSize == "" {
		if description != "" {
			avatarSize = "md"
		} else {
			avatarSize = "sm"
		}
	}
	return Data{
		"alertVariant":        buttonOption(props.String("variant"), "info", "info", "success", "warning", "error"),
		"alertSize":           buttonOption(props.String("size"), "md", "sm", "md", "lg"),
		"alertRounded":        defaultBool(props, "rounded", true),
		"alertClosable":       props.Bool("closable"),
		"alertTitle":          strings.TrimSpace(props.String("title")),
		"alertDescription":    description,
		"alertIcon":           strings.TrimSpace(props.String("icon")),
		"alertHasTitle":       strings.TrimSpace(props.String("title")) != "",
		"alertHasDescription": description != "",
		"alertHasAvatar":      hasAvatar,
		"alertAvatarSrc":      avatarSrc,
		"alertAvatarIcon":     avatarIcon,
		"alertAvatarAlt":      strings.TrimSpace(props.String("avatarAlt")),
		"alertAvatarSize":     avatarSize,
	}, nil
}

func (UiAvatarData) Render(_ *Context, props Props) (Data, error) {
	src := strings.TrimSpace(props.String("src"))
	icon := strings.TrimSpace(props.String("icon"))
	initials := avatarInitials(props.String("text"), props.String("name"))
	hasImage := src != ""
	hasIcon := !hasImage && icon != ""
	hasInitials := !hasImage && !hasIcon && initials != ""
	return Data{
		"avatarSize":         buttonOption(props.String("size"), "md", "sm", "md", "lg", "xl"),
		"avatarChipPosition": buttonOption(props.String("chipPosition"), "top-right", "top-right", "bottom-right", "top-left", "bottom-left"),
		"avatarInitials":     initials,
		"avatarIcon":         icon,
		"avatarSrc":          src,
		"avatarAlt":          defaultString(strings.TrimSpace(props.String("alt")), defaultString(strings.TrimSpace(props.String("name")), "avatar")),
		"avatarHasInitials":  hasInitials,
		"avatarHasIcon":      hasIcon,
		"avatarHasImage":     hasImage,
		"avatarChipColor":    strings.TrimSpace(props.String("chipColor")),
		"avatarHasChip":      strings.TrimSpace(props.String("chipColor")) != "",
	}, nil
}

func (UiBadgeData) Render(_ *Context, props Props) (Data, error) {
	avatarSrc := strings.TrimSpace(props.String("avatarSrc"))
	avatarIcon := strings.TrimSpace(props.String("avatarIcon"))
	hasAvatar := avatarSrc != "" || avatarIcon != ""
	leading := strings.TrimSpace(props.String("leadingIcon"))
	if leading == "" {
		leading = strings.TrimSpace(props.String("icon"))
	}
	showLeading := defaultBool(props, "showLeading", true) && leading != "" && !hasAvatar
	trailing := strings.TrimSpace(props.String("trailingIcon"))
	return Data{
		"badgeColor":        buttonOption(props.String("color"), "gray", "primary", "gray", "info", "success", "warning", "error"),
		"badgeVariant":      buttonOption(props.String("variant"), "soft", "solid", "soft", "outline"),
		"badgeSize":         buttonOption(props.String("size"), "sm", "xs", "sm", "md"),
		"badgeRounded":      defaultBool(props, "rounded", true),
		"badgePill":         props.Bool("pill"),
		"badgeLabel":        strings.TrimSpace(props.String("label")),
		"badgeLeadingIcon":  leading,
		"badgeTrailingIcon": trailing,
		"badgeShowLeading":  showLeading,
		"badgeShowTrailing": defaultBool(props, "showTrailing", true) && trailing != "",
		"badgeHasAvatar":    hasAvatar,
		"badgeAvatarSrc":    avatarSrc,
		"badgeAvatarIcon":   avatarIcon,
		"badgeAvatarAlt":    strings.TrimSpace(props.String("avatarAlt")),
	}, nil
}

func (UiBannerData) Render(_ *Context, props Props) (Data, error) {
	avatarSrc := strings.TrimSpace(props.String("avatarSrc"))
	avatarIcon := strings.TrimSpace(props.String("avatarIcon"))
	hasAvatar := avatarSrc != "" || avatarIcon != ""
	icon := strings.TrimSpace(props.String("icon"))
	return Data{
		"bannerColor":          buttonOption(props.String("color"), "gray", "primary", "gray", "info", "success", "warning", "error"),
		"bannerVariant":        buttonOption(props.String("variant"), "soft", "solid", "soft", "outline"),
		"bannerClosable":       props.Bool("closable"),
		"bannerRounded":        defaultBool(props, "rounded", true),
		"bannerBordered":       props.Bool("bordered"),
		"bannerTitle":          strings.TrimSpace(props.String("title")),
		"bannerDescription":    strings.TrimSpace(props.String("description")),
		"bannerHasTitle":       strings.TrimSpace(props.String("title")) != "",
		"bannerHasDescription": strings.TrimSpace(props.String("description")) != "",
		"bannerIcon":           icon,
		"bannerHasMedia":       hasAvatar || icon != "",
		"bannerHasAvatar":      hasAvatar,
		"bannerAvatarSrc":      avatarSrc,
		"bannerAvatarIcon":     avatarIcon,
		"bannerAvatarAlt":      strings.TrimSpace(props.String("avatarAlt")),
	}, nil
}

func (UiTruncateTextData) Render(_ *Context, props Props) (Data, error) {
	mobile, desktop := props.Int("mobileLimit"), props.Int("desktopLimit")
	if mobile <= 0 {
		mobile = desktop
	}
	if desktop <= 0 {
		desktop = mobile
	}
	return Data{
		"truncateMobileLimit":  mobile,
		"truncateDesktopLimit": desktop,
		"truncateExpandText":   defaultString(strings.TrimSpace(props.String("expandText")), "Развернуть"),
		"truncateCollapseText": defaultString(strings.TrimSpace(props.String("collapseText")), "Свернуть"),
	}, nil
}

func (UiTooltipData) Render(_ *Context, props Props) (Data, error) {
	trigger := buttonOption(props.String("trigger"), "hover", "hover", "click")
	role := buttonOption(props.String("role"), "tooltip", "tooltip", "dialog")
	placement := buttonOption(props.String("placement"), "auto", "auto", "top", "right", "bottom", "left", "top-start", "top-end", "right-start", "right-end", "bottom-start", "bottom-end", "left-start", "left-end")
	disabled := props.Bool("disabled")
	return Data{
		"tooltipAsDiv":           strings.EqualFold(props.String("as"), "div"),
		"tooltipContent":         strings.TrimSpace(props.String("content")),
		"tooltipTrigger":         trigger,
		"tooltipPlacement":       placement,
		"tooltipOffset":          maxInt(props.Int("offset"), 10),
		"tooltipViewportPadding": maxInt(props.Int("viewportPadding"), 8),
		"tooltipOpenDelay":       maxInt(props.Int("openDelay"), 100),
		"tooltipCloseDelay":      maxInt(props.Int("closeDelay"), 120),
		"tooltipDisabled":        disabled,
		"tooltipInteractive":     defaultBool(props, "interactive", true),
		"tooltipMaxWidth":        defaultString(strings.TrimSpace(props.String("maxWidth")), "24rem"),
		"tooltipRole":            role,
		"tooltipTabindex":        tooltipTabindex(props.Int("triggerTabindex"), disabled),
	}, nil
}

func (UiTabsData) Render(_ *Context, props Props) (Data, error) {
	orientation := buttonOption(props.String("orientation"), "vertical", "vertical", "horizontal")
	side := strings.ToLower(strings.TrimSpace(props.String("side")))
	if orientation == "vertical" && side != "right" {
		side = "left"
	}
	if orientation == "horizontal" && side != "bottom" {
		side = "top"
	}
	return Data{
		"tabsOrientation":   orientation,
		"tabsSide":          side,
		"tabsTabWidth":      buttonOption(props.String("tabWidth"), "full", "full", "auto"),
		"tabsAlign":         buttonOption(props.String("align"), "start", "start", "center", "end"),
		"tabsWidthStyle":    "--ui-tabs-list-width:" + defaultString(strings.TrimSpace(props.String("tabsWidth")), "16rem"),
		"tabsLoop":          defaultBool(props, "loop", true),
		"tabsStackOnMobile": defaultBool(props, "stackOnMobile", true),
		"tabsAriaLabel":     defaultString(strings.TrimSpace(props.String("ariaLabel")), "Tabs"),
	}, nil
}

func (UiTableData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"tableHeightStyle": "--ui-table-height:" + defaultString(strings.TrimSpace(props.String("height")), "auto"),
		"tableEmptyText":   defaultString(strings.TrimSpace(props.String("emptyText")), "Нет данных"),
	}, nil
}

func (UiStickyWrapperData) Render(_ *Context, props Props) (Data, error) {
	offset := 100
	if _, exists := props["offset"]; exists {
		offset = props.Int("offset")
		if offset < 0 {
			offset = 0
		}
	}
	return Data{
		"stickyWrapperStyle": fmt.Sprintf("--ui-sticky-offset:%dpx;--ui-sticky-max-height:calc(100dvh - %dpx)", offset, offset+20),
	}, nil
}

func (UiSplitSectionData) Render(_ *Context, props Props) (Data, error) {
	containerSize := LayoutSize(props.String("containerSize"), LayoutSizeBoxed)
	imagePosition := buttonOption(props.String("imgPosition"), "left", "left", "right")
	textMaxWidth := splitSectionTextMaxWidth(containerSize)
	style := ""
	if textMaxWidth != "" {
		style = "--ui-split-section-text-max-width:" + textMaxWidth
	}
	return Data{
		"splitImageSrc":         strings.TrimSpace(props.String("imageSrc")),
		"splitImageAlt":         strings.TrimSpace(props.String("imageAlt")),
		"splitImagePosition":    imagePosition,
		"splitContainerSize":    containerSize,
		"splitImageToEdge":      props.Bool("imageToEdge"),
		"splitReverseOnMobile":  props.Bool("reverseOnMobile"),
		"splitImageClass":       strings.TrimSpace(props.String("imageClass")),
		"splitContainerIsFull":  containerSize == "full" || containerSize == "fullwidth",
		"splitSectionTextStyle": style,
	}, nil
}

func (UiSmartContrastData) Render(_ *Context, props Props) (Data, error) {
	style := "--ui-smart-contrast-height:" + safeCSSSize(props.String("height"), "100vh")
	if source := strings.TrimSpace(props.String("src")); source != "" {
		style += ";background-image:url('" + strings.ReplaceAll(strings.ReplaceAll(source, "\\", "\\\\"), "'", "\\'") + "')"
	} else if gradient := safeCSSBackground(props.String("gradient")); gradient != "" {
		style += ";background:" + gradient
	}
	if props.Bool("fixed") {
		style += ";background-attachment:fixed"
	}
	return Data{
		"smartContrastStyle": style,
		"smartContrastAuto":  defaultBool(props, "auto", true),
	}, nil
}

func (UiSnapContainerData) Render(_ *Context, props Props) (Data, error) {
	return Data{"snapDirection": buttonOption(props.String("direction"), "vertical", "vertical", "horizontal")}, nil
}

func (UiSnapSectionData) Render(_ *Context, props Props) (Data, error) {
	videoID, mobileVideoID := youtubeID(props.String("youtubeId")), youtubeID(props.String("youtubeIdMobile"))
	overlay := strings.TrimSpace(props.String("overlay"))
	overlayStyle := ""
	if overlay != "" && !strings.EqualFold(overlay, "false") {
		if strings.EqualFold(overlay, "true") {
			overlay = "rgba(0, 0, 0, .4)"
		}
		overlayStyle = safeCSSBackground(overlay)
	}
	return Data{
		"snapSectionBackground":    strings.TrimSpace(props.String("bgImage")),
		"snapSectionVideo":         strings.TrimSpace(props.String("bgVideo")),
		"snapSectionVideoMobile":   strings.TrimSpace(props.String("bgVideoMobile")),
		"snapSectionPoster":        strings.TrimSpace(props.String("poster")),
		"snapSectionPosterMobile":  strings.TrimSpace(props.String("posterMobile")),
		"snapSectionYoutube":       youtubeEmbed(videoID),
		"snapSectionYoutubeMobile": youtubeEmbed(mobileVideoID),
		"snapSectionOverlayStyle":  overlayStyle,
		"snapSectionHasOverlay":    overlayStyle != "",
		"snapSectionLoading":       snapSectionLoading(defaultBool(props, "lazy", true)),
		"snapSectionContentClass":  strings.TrimSpace(props.String("contentClass")),
	}, nil
}

func (UiSegmentedControlData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"segmentedMultiple": defaultBool(props, "multiple", false),
		"segmentedVertical": defaultBool(props, "vertical", false),
		"segmentedFull":     defaultBool(props, "full", false),
		"segmentedColor":    buttonOption(props.String("color"), "gray", "gray", "primary", "info", "success", "warning", "error"),
		"segmentedVariant":  buttonOption(props.String("variant"), "outline", "outline", "soft"),
		"segmentedSize":     buttonOption(props.String("size"), "md", "xs", "sm", "md", "lg"),
		"segmentedLabel":    defaultString(strings.TrimSpace(props.String("ariaLabel")), "Segmented control"),
	}, nil
}

func (UiSegmentedControlItemData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"segmentedItemValue":    defaultString(strings.TrimSpace(props.String("value")), strings.TrimSpace(props.String("label"))),
		"segmentedItemLabel":    strings.TrimSpace(props.String("label")),
		"segmentedItemIcon":     strings.TrimSpace(props.String("icon")),
		"segmentedItemActive":   defaultBool(props, "active", false),
		"segmentedItemPressed":  fmt.Sprintf("%t", defaultBool(props, "active", false)),
		"segmentedItemDisabled": props.Bool("disabled"),
	}, nil
}

func (UiCardData) Render(_ *Context, props Props) (Data, error) {
	href := strings.TrimSpace(props.String("href"))
	if href == "" {
		href = strings.TrimSpace(props.String("to"))
	}
	as := buttonOption(props.String("as"), "div", "div", "section", "article")
	target := strings.TrimSpace(props.String("target"))
	return Data{
		"cardIsLink":    href != "",
		"cardHref":      href,
		"cardTarget":    target,
		"cardRel":       cardRel(target, strings.TrimSpace(props.String("rel"))),
		"cardIsDiv":     as == "div",
		"cardIsSection": as == "section",
		"cardIsArticle": as == "article",
		"cardBordered":  defaultBool(props, "bordered", true),
		"cardRounded":   defaultBool(props, "rounded", true),
		"cardPadded":    defaultBool(props, "padded", true),
	}, nil
}

func (UiChipData) Render(_ *Context, props Props) (Data, error) {
	text := imagePropString(props, "text")
	return Data{
		"chipText":       text,
		"chipColor":      buttonOption(props.String("color"), "primary", "primary", "red", "green", "blue", "gray", "neutral"),
		"chipSize":       buttonOption(props.String("size"), "sm", "xs", "sm", "md", "lg"),
		"chipPosition":   buttonOption(props.String("position"), "top-right", "top-right", "top-left", "bottom-right", "bottom-left"),
		"chipInset":      props.Bool("inset"),
		"chipStandalone": props.Bool("standalone"),
		"chipShow":       defaultBool(props, "show", true),
		"chipPing":       props.Bool("ping"),
		"chipLongText":   len([]rune(text)) > 2,
	}, nil
}

func (UiTextColumnData) Render(_ *Context, props Props) (Data, error) {
	classes := []string{props.String("class"), props.String("textSizeClass"), props.String("textColorClass"), props.String("spacingClass")}
	return Data{"textColumnClasses": strings.TrimSpace(strings.Join(classes, " "))}, nil
}

func (UiHeadingData) Render(_ *Context, props Props) (Data, error) {
	level := props.Int("level")
	if level < 1 || level > 6 {
		level = 2
	}
	size := buttonOption(props.String("size"), "", "", "xs", "sm", "md", "lg", "xl", "display")
	return Data{
		"headingLevel": level,
		"headingSize":  size,
		"isH1":         level == 1,
		"isH2":         level == 2,
		"isH3":         level == 3,
		"isH4":         level == 4,
		"isH5":         level == 5,
		"isH6":         level == 6,
	}, nil
}

func (UiStepsData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"stepsVariant":         buttonOption(props.String("variant"), "steps", "steps", "progress", "cards"),
		"stepsPlacement":       buttonOption(props.String("placement"), "top", "top", "bottom"),
		"stepsAlign":           buttonOption(props.String("align"), "start", "start", "center", "end"),
		"stepsClickable":       defaultBool(props, "clickable", true),
		"stepsLinear":          props.Bool("linear"),
		"stepsRequirePrevious": props.Bool("requirePrevious"),
		"stepsStackOnMobile":   defaultBool(props, "stackOnMobile", true),
		"stepsAriaLabel":       defaultString(props.String("ariaLabel"), "Progress steps"),
	}, nil
}

func (UiStepData) Render(_ *Context, props Props) (Data, error) {
	title := defaultString(props.String("title"), props.String("label"))
	value := defaultString(props.String("value"), title)
	icon := strings.TrimSpace(props.String("icon"))
	return Data{
		"stepValue":            value,
		"stepTitle":            title,
		"stepDescription":      strings.TrimSpace(props.String("description")),
		"stepContent":          props.String("content"),
		"stepIcon":             icon,
		"stepHasIcon":          icon != "",
		"stepDisabled":         props.Bool("disabled"),
		"stepActive":           props.Bool("active"),
		"stepCompleted":        props.Bool("completed"),
		"stepRequires":         strings.TrimSpace(props.String("requires")),
		"stepRequiresPrevious": props.Bool("requiresPrevious"),
	}, nil
}

func (UiScrollToTopData) Render(_ *Context, props Props) (Data, error) {
	threshold := props.Int("threshold")
	if threshold < 0 {
		threshold = 0
	}
	if threshold == 0 {
		threshold = 600
	}
	return Data{
		"scrollToTopThreshold": threshold,
		"scrollToTopLabel":     defaultString(props.String("ariaLabel"), "Scroll to top"),
		"scrollToTopBehavior":  buttonOption(props.String("behavior"), "smooth", "smooth", "auto"),
	}, nil
}

func (UiProtectedEmailData) Render(_ *Context, props Props) (Data, error) {
	return Data{
		"emailUser":        strings.TrimSpace(props.String("user")),
		"emailDomain":      strings.TrimSpace(props.String("domain")),
		"emailTLD":         defaultString(strings.TrimSpace(props.String("tld")), "com"),
		"emailSubject":     strings.TrimSpace(props.String("subject")),
		"emailPlaceholder": defaultString(props.String("placeholder"), "[email hidden]"),
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func imagePropString(props Props, name string) string {
	value, ok := props[name]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
func defaultBool(props Props, name string, fallback bool) bool {
	value, exists := props[name]
	if !exists {
		return fallback
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return !strings.EqualFold(strings.TrimSpace(v), "false")
	default:
		return fallback
	}
}
func buttonOption(value, fallback string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if value == item {
			return value
		}
	}
	return fallback
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func normalizeIconSize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if iconPixelSizePattern.MatchString(value) {
		return value + "px"
	}
	if iconSizePattern.MatchString(value) {
		return value
	}
	return ""
}

func viewportSpacerSize(value, fallback string) string {
	if match := viewportSpacerSizePattern.FindStringSubmatch(value); len(match) == 2 {
		return match[1]
	}
	return fallback
}

func fileUploadRole(interactive, disabled bool) string {
	if interactive && !disabled {
		return "button"
	}
	return ""
}

func fileUploadTabindex(interactive, disabled bool) string {
	if interactive && !disabled {
		return "0"
	}
	return ""
}

func avatarInitials(text, name string) string {
	if text = strings.TrimSpace(text); text != "" {
		runes := []rune(text)
		return strings.ToUpper(string(runes[:min(len(runes), 2)]))
	}
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 {
		return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[1])[0]))
	}
	runes := []rune(parts[0])
	return strings.ToUpper(string(runes[:min(len(runes), 2)]))
}

func tooltipTabindex(value int, disabled bool) string {
	if disabled {
		return "-1"
	}
	if value == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", value)
}

func checkboxID(name, value string) string {
	source := strings.TrimSpace(name + "-" + value)
	if source == "-" {
		source = "checkbox"
	}
	var b strings.Builder
	for _, r := range source {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return "ui-checkbox-" + strings.Trim(b.String(), "-")
}

func splitSectionTextMaxWidth(containerSize string) string {
	return LayoutContainerHalfWidth(containerSize)
}

func safeCSSSize(value, fallback string) string {
	value = strings.TrimSpace(value)
	if regexp.MustCompile(`^\d+(?:\.\d+)?(?:px|rem|em|vh|dvh|vw|%|ch)$`).MatchString(value) {
		return value
	}
	return fallback
}

func safeCSSBackground(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ";{}<>\\") {
		return ""
	}
	return value
}

func snapSectionLoading(lazy bool) string {
	if lazy {
		return "lazy"
	}
	return "eager"
}

func cardRel(target, rel string) string {
	if rel != "" || target != "_blank" {
		return rel
	}
	return "noopener noreferrer"
}

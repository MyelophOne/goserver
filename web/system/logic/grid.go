package systemlogic

import (
	"net/url"
	"regexp"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type GridBackground struct{ Noop }

var youtubeIDPattern = regexp.MustCompile("(?:youtu\\.be/|/v/|/u/\\w/|/embed/|watch\\?v=|[?&]v=)([^#&?]+)")

func (GridBackground) Render(_ *Context, props Props) (Data, error) {
	videoID, mobileVideoID := youtubeID(props.String("bgYoutube")), youtubeID(props.String("bgYoutubeSm"))
	return Data{"youtubeEmbed": youtubeEmbed(videoID), "youtubeEmbedSm": youtubeEmbed(mobileVideoID), "youtubePoster": youtubePoster(videoID), "youtubePosterSm": youtubePoster(mobileVideoID), "youtubeZoom": defaultBool(props, "bgYoutubeZoom", false), "layoutSize": LayoutSize(props.String("size"), LayoutSizeContent)}, nil
}

func youtubeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return ""
	}
	if match := youtubeIDPattern.FindStringSubmatch(value); len(match) == 2 {
		value = match[1]
	}
	if len(value) != 11 {
		return ""
	}
	return value
}
func youtubeEmbed(id string) string {
	if id == "" {
		return ""
	}
	params := url.Values{"autoplay": {"1"}, "mute": {"1"}, "controls": {"0"}, "loop": {"1"}, "playlist": {id}, "playsinline": {"1"}, "rel": {"0"}, "modestbranding": {"1"}, "iv_load_policy": {"3"}, "autohide": {"1"}, "disablekb": {"1"}, "enablejsapi": {"1"}}
	return "https://www.youtube-nocookie.com/embed/" + id + "?" + params.Encode()
}
func youtubePoster(id string) string {
	if id == "" {
		return ""
	}
	return "https://img.youtube.com/vi/" + id + "/maxresdefault.jpg"
}

package systemlogic

import (
	"runtime/debug"
	"strings"

	. "github.com/myelophone/goserver/web/runtime"
)

type WelcomeComponent struct{ Noop }

var BuildVersion string

func (WelcomeComponent) Render(ctx *Context, _ Props) (Data, error) {
	return Data{
		"welcomeTitle":           welcomeText(ctx, "welcome", "Welcome to"),
		"welcomeExperiment":      welcomeText(ctx, "experiment", "Experiment, customize and enjoy the speed and the performance!"),
		"welcomePoweredBy":       welcomeText(ctx, "poweredBy", "Powered by"),
		"welcomeGoServerVersion": welcomeText(ctx, "goserverVersion", "GoServer version"),
		"welcomeToggleTheme":     welcomeText(ctx, "toggleTheme", "Toggle theme"),
		"welcomeVersion":         goserverVersion(),
	}, nil
}

func welcomeText(ctx *Context, key, fallback string) string {
	translationKey := "common.myelophone." + key
	value := ctx.T(translationKey)
	if value == "" || value == translationKey {
		return fallback
	}
	return value
}

func goserverVersion() string {
	if BuildVersion != "" {
		return BuildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return goserverBuildVersion(info)
}

func goserverBuildVersion(info *debug.BuildInfo) string {
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/myelophone/goserver" {
			if dependency.Replace != nil && dependency.Replace.Version != "" {
				return dependency.Replace.Version
			}
			if dependency.Version != "" && dependency.Version != "(devel)" {
				return dependency.Version
			}
			return "dev"
		}
	}
	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value[:min(len(setting.Value), 7)]
		}
	}
	return "dev"
}

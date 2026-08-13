package goserver

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 16_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",

	"Mozilla/5.0 (Windows NT 11.0; Win64; x64; rv:146.0) Gecko/20100101 Firefox/146.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 16.2; rv:146.0) Gecko/20100101 Firefox/146.0",
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64; rv:147.0) Gecko/20100101 Firefox/147.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 16.2; rv:147.0) Gecko/20100101 Firefox/147.0",
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 16.2; rv:148.0) Gecko/20100101 Firefox/148.0",

	"Mozilla/5.0 (Macintosh; Intel Mac OS X 16_2) AppleWebKit/611.1.21 (KHTML, like Gecko) Version/19.2 Safari/611.1.21",

	"Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",

	"Mozilla/5.0 (Linux; Android 16; Pixel 10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 15; SM-S938B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Mobile Safari/537.36",

	"Mozilla/5.0 (iPhone; CPU iPhone OS 19_2 like Mac OS X) AppleWebKit/611.1.21 (KHTML, like Gecko) Version/19.2 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (iPad; CPU OS 19_2 like Mac OS X) AppleWebKit/611.1.21 (KHTML, like Gecko) CriOS/146.0.0.0 Mobile/15E148 Safari/604.1",
}

var defaultHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"Connection":                "keep-alive",
	"Cache-Control":             "no-cache",
	"Pragma":                    "no-cache",
	"DNT":                       "1",
	"Upgrade-Insecure-Requests": "1",
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
}

var acceptLanguages = []string{
	"en-US,en;q=0.9",
	"en-GB,en-US;q=0.9,en;q=0.8",
	"ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7",
	"es-ES,es;q=0.9,en-US;q=0.8,en;q=0.7",
	"fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7",
	"de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7",
	"pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
	"it-IT,it;q=0.9,en-US;q=0.8,en;q=0.7",
	"tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7",
	"uk-UA,uk;q=0.9,ru;q=0.8,en-US;q=0.7,en;q=0.6",
}

var (
	chromeRegex = regexp.MustCompile(`Chrome/(\d+)`)
	edgeRegex   = regexp.MustCompile(`Edg/(\d+)`)
)

func randomAcceptLanguage() string {
	r := rngPool.Get().(*rand.Rand)
	lang := acceptLanguages[r.Intn(len(acceptLanguages))]
	rngPool.Put(r)
	return lang
}

var rngPool = sync.Pool{
	New: func() any {
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	},
}

func randomUserAgent() string {
	r := rngPool.Get().(*rand.Rand)
	ua := userAgents[r.Intn(len(userAgents))]
	rngPool.Put(r)
	return ua
}

func generateSecChHeaders(userAgent string) (secChUa, secChUaMobile, secChUaPlatform string) {
	switch {
	case strings.Contains(userAgent, "Windows"):
		secChUaPlatform = `"Windows"`
	case strings.Contains(userAgent, "Macintosh") || strings.Contains(userAgent, "Mac OS"):
		secChUaPlatform = `"macOS"`
	case strings.Contains(userAgent, "Android"):
		secChUaPlatform = `"Android"`
	case strings.Contains(userAgent, "iPhone") || strings.Contains(userAgent, "iPad"):
		secChUaPlatform = `"iOS"`
	case strings.Contains(userAgent, "Linux"):
		secChUaPlatform = `"Linux"`
	default:
		secChUaPlatform = `"Unknown"`
	}

	if strings.Contains(userAgent, "Mobile") || secChUaPlatform == `"Android"` || secChUaPlatform == `"iOS"` {
		secChUaMobile = "?1"
	} else {
		secChUaMobile = "?0"
	}

	if match := edgeRegex.FindStringSubmatch(userAgent); len(match) > 1 {
		version := match[1]
		secChUa = fmt.Sprintf(`"Chromium";v="%s", "Microsoft Edge";v="%s", "Not=A?Brand";v="99"`, version, version)
		return
	}

	if match := chromeRegex.FindStringSubmatch(userAgent); len(match) > 1 {
		version := match[1]
		secChUa = fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not=A?Brand";v="99"`, version, version)
		return
	}

	secChUa = `""`
	return
}

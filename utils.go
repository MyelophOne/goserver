package goserver

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func GetEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	valStr := GetEnv(key, "")
	if valStr == "" {
		return defaultValue
	}

	if d, err := time.ParseDuration(valStr); err == nil {
		return d
	}

	if sec, err := strconv.Atoi(valStr); err == nil {
		return time.Duration(sec) * time.Second
	}

	return defaultValue
}

func GetEnvInt(key string, defaultValue int) int {
	valStr := GetEnv(key, "")
	if valStr == "" {
		return defaultValue
	}

	if v, err := strconv.Atoi(valStr); err == nil {
		return v
	}
	return defaultValue
}

func GetEnvBytes(key string, defaultValue int64) int64 {
	val := GetEnv(key, "")
	if val == "" {
		return defaultValue
	}

	val = strings.TrimSpace(strings.ToUpper(val))

	if strings.Contains(val, "<<") {
		parts := strings.Split(val, "<<")
		if len(parts) == 2 {
			left, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			right, err2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 6)
			if err1 == nil && err2 == nil {
				return left << right
			}
		}
		return defaultValue
	}

	multiplier := int64(1)

	switch {
	case strings.HasSuffix(val, "KB"):
		multiplier = 1024
		val = strings.TrimSuffix(val, "KB")
	case strings.HasSuffix(val, "MB"):
		multiplier = 1024 * 1024
		val = strings.TrimSuffix(val, "MB")
	case strings.HasSuffix(val, "GB"):
		multiplier = 1024 * 1024 * 1024
		val = strings.TrimSuffix(val, "GB")
	}

	num, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil {
		return defaultValue
	}

	return num * multiplier
}

func GetEnvBytesInt(key string, defaultValue int) int {
	return int(GetEnvBytes(key, int64(defaultValue)))
}

func GetEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func GetResponseMode() string {
	value := strings.ToLower(GetEnv("APP_ERROR_MODE", "html"))

	switch value {
	case "html", "json":
		return value
	default:
		return "html"
	}
}

func generateRandomKey() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		panic("failed to generate random key: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(b)
}

func maskPartially(s string) string {
	if len(s) <= 4 {
		return "[***]"
	}
	return "[" + s[:3] + "...]"
}

func GetRealIP(r *http.Request) string {
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		ips := splitAndTrim(forwardedFor, ",")
		if len(ips) > 0 {
			ip := trimSpace(ips[0])
			if IsValidIP(ip) {
				return ip
			}
		}
	}

	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		realIP = trimSpace(realIP)
		if IsValidIP(realIP) {
			return realIP
		}
	}
	forwarded := r.Header.Get("X-Forwarded")
	if forwarded != "" {
		parts := splitAndTrim(forwarded, ";")
		for _, part := range parts {
			if ipPart, ok := strings.CutPrefix(part, "for="); ok {
				ip := trimSpace(ipPart)
				ip = strings.Trim(ip, `"`)
				if IsValidIP(ip) {
					return ip
				}
			}
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = trimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

func IsValidIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil
}

func isValidOrigin(origin string, validHost string) bool {
	if IsDev() {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if strings.HasPrefix(u.Host, "localhost") || strings.HasPrefix(u.Host, "127.0.0.1") {
		return true
	}

	ip := net.ParseIP(u.Host)
	if ip.IsLoopback() {
		return true
	}

	return u.Host == validHost
}

func isRequestMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		return true
	default:
		return false
	}
}

func toASCII(input []byte) []byte {
	var out bytes.Buffer
	for _, b := range input {
		if b < 128 {
			out.WriteByte(b)
		} else {
			out.WriteString(`\u`)
			out.WriteString(hex4(b))
		}
	}
	return out.Bytes()
}

func hex4(b byte) string {
	const hex = "0123456789abcdef"
	return string([]byte{
		'0', '0',
		hex[b>>4],
		hex[b&0xF],
	})
}

func SanitizeXSS(input string) string {
	xssPattern := regexp.MustCompile(`(?i)<.*?>|javascript:|on\w+=[^\s>]+|alert\(|prompt\(|confirm\(|eval\(|setTimeout\(|setInterval\(`)
	return xssPattern.ReplaceAllString(input, "")
}

func SanitizeXSSQuery(values url.Values) url.Values {
	for key, val := range values {
		for i, v := range val {
			values[key][i] = SanitizeXSS(v)
		}
	}
	return values
}

func FormatBytes(b uint64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)

	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func getFirst(query url.Values, key string) (string, bool) {
	vals, ok := query[key]
	if !ok || len(vals) == 0 || vals[0] == "" {
		return "", false
	}
	return vals[0], true
}

func GetQueryParam(query url.Values, key string, defaultValue string, allowed []string) string {
	val, ok := getFirst(query, key)
	if !ok {
		return defaultValue
	}

	if allowed == nil {
		return val
	}

	for _, a := range allowed {
		if val == a {
			return val
		}
	}

	return defaultValue
}

func GetQueryArray(query url.Values, key string, defaultValue []string, allowed []string) []string {
	vals, ok := query[key]
	if !ok || len(vals) == 0 {
		return defaultValue
	}

	if allowed == nil {
		result := make([]string, 0, len(vals))
		for _, v := range vals {
			if v != "" {
				result = append(result, v)
			}
		}
		return result
	}

	allowedMap := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedMap[a] = struct{}{}
	}

	result := make([]string, 0, len(vals))
	for _, v := range vals {
		if v == "" {
			continue
		}
		if _, ok := allowedMap[v]; ok {
			result = append(result, v)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}

func ExtractJSVar(html []byte, varName []byte) []byte {
	offset := 0
	for {
		idx := bytes.Index(html[offset:], varName)
		if idx == -1 {
			return nil
		}
		absIdx := offset + idx
		offset = absIdx + len(varName)

		if absIdx > 0 {
			prev := html[absIdx-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') {
				continue
			}
		}

		var eqIdx = -1
		for i := absIdx + len(varName); i < len(html); i++ {
			c := html[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				continue
			}
			if c == '=' {
				eqIdx = i
				break
			}
			break
		}

		if eqIdx == -1 {
			continue
		}

		var startIdx = -1
		for i := eqIdx + 1; i < len(html); i++ {
			c := html[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				continue
			}
			if c == '{' || c == '[' {
				startIdx = i
				break
			}
			break
		}

		if startIdx == -1 {
			continue
		}

		depth := 0
		inString := false
		escaped := false

		for i := startIdx; i < len(html); i++ {
			c := html[i]

			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && inString {
				escaped = true
				continue
			}
			if c == '"' {
				inString = !inString
				continue
			}

			if !inString {
				switch c {
				case '{', '[':
					depth++
				case '}', ']':
					depth--
					if depth == 0 {
						return html[startIdx : i+1]
					}
				}
			}
		}
		return nil
	}
}

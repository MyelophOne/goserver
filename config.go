package goserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LogLevel                 string
	LogClientIP              string
	APIPrefix                string
	WsTokenKey               string
	CsrfTrustedOrigins       string
	DatabaseUrl              string
	PostgresHost             string
	PostgresUser             string
	PostgresPassword         string
	PostgresDb               string
	DbExecMode               string
	DbMaxConns               string
	DbMinConns               string
	DbLogMode                string
	SmtpHost                 string
	SmtpPort                 string
	SmtpUser                 string
	SmtpPassword             string
	SmtpFrom                 string
	SmtpQueueSize            string
	SmtpWorkers              string
	I18nDefaultLanguage      string
	I18nLanguages            []string
	MaintenanceCheckInterval time.Duration
	sessionKey               string
	TZ                       string
	JWTSecret                string
	metricsToken             string
	MaxURLLength             int
	MaxHeaders               int
	MaxConnections           int64
	ReadTimeout              time.Duration
	WriteTimeout             time.Duration
	IdleTimeout              time.Duration
	ReadHeaderTimeout        time.Duration
	MaxHeaderBytes           int
	ShutdownTimeout          time.Duration
	PingTimeout              time.Duration
	WriteByteTimeout         time.Duration
	maxConcurrent            int
	MaxBodySize              int64
	RateLimiteSize           int
	RateLimiteRate           int
	RateLimiteWindow         time.Duration
	EnableSlowlorisCheck     bool
	MaintenanceMode          bool
	maintenanceBypassToken   string
	RateLimitSkipLocalhost   bool
	EnableGzip               bool
	metricsEnabled           bool
}

func (s *Server) loadConfig() {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		s.Logger.Fatal(err.Error())
	}

	s.Config = Config{
		LogLevel:                 ParseLogLevel(GetEnv("LOG_LEVEL", "info")).String(),
		LogClientIP:              ParseClientIPLogMode(GetEnv("LOG_CLIENT_IP", "off")).String(),
		APIPrefix:                GetEnv("API_PREFIX", ""),
		MaxURLLength:             GetEnvInt("MAX_URL_LENGTH", 2048),
		MaxHeaders:               GetEnvInt("MAX_HEADERS", 100),
		MaxConnections:           int64(GetEnvInt("MAX_CONNECTIONS", 10000)),
		ReadTimeout:              GetEnvDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:             GetEnvDuration("WRITE_TIMEOUT", 15*time.Second),
		WriteByteTimeout:         GetEnvDuration("WRITE_BYTE_TIMEOUT", 5*time.Second),
		IdleTimeout:              GetEnvDuration("IDLE_TIMEOUT", 90*time.Second),
		PingTimeout:              GetEnvDuration("PING_TIMEOUT", 15*time.Second),
		ReadHeaderTimeout:        GetEnvDuration("READ_HEADER_TIMEOUT", 500*time.Millisecond),
		MaxHeaderBytes:           GetEnvInt("MAX_HEADER_BYTES", 1<<16),
		ShutdownTimeout:          GetEnvDuration("RELOAD_SHUTDOWN_TIMEOUT", 30*time.Second),
		EnableSlowlorisCheck:     GetEnv("ENABLE_SLOWLORIS_CHECK", "false") == "true",
		MaintenanceMode:          GetEnvBool("MAINTENANCE_MODE", false),
		MaintenanceCheckInterval: GetEnvDuration("MAINTENANCE_CHECK_INTERVAL", 5*time.Second),
		maintenanceBypassToken:   GetEnv("MAINTENANCE_BYPASS_TOKEN", ""),
		WsTokenKey:               GetEnv("WS_TOKEN_KEY", generateRandomKey()),
		CsrfTrustedOrigins:       GetEnv("CSRF_TRUSTED_ORIGINS", ""),
		DatabaseUrl:              GetEnv("DATABASE_URL", ""),
		PostgresHost:             GetEnv("POSTGRES_HOST", ""),
		PostgresUser:             GetEnv("POSTGRES_USER", "postgres"),
		PostgresPassword:         GetEnv("POSTGRES_PASSWORD", ""),
		PostgresDb:               GetEnv("POSTGRES_DB", "postgres"),
		DbExecMode:               GetEnv("DB_EXEC_MODE", ""),
		DbMaxConns:               GetEnv("DB_MAX_CONNS", ""),
		DbMinConns:               GetEnv("DB_MIN_CONNS", ""),
		DbLogMode:                GetEnv("DB_LOG_MODE", "sanitized"),
		SmtpHost:                 GetEnv("SMTP_HOST", ""),
		SmtpPort:                 GetEnv("SMTP_PORT", ""),
		SmtpUser:                 GetEnv("SMTP_USER", ""),
		SmtpPassword:             GetEnv("SMTP_PASS", ""),
		SmtpFrom:                 GetEnv("SMTP_FROM", ""),
		SmtpWorkers:              GetEnv("SMTP_WORKERS", "1"),
		SmtpQueueSize:            GetEnv("SMTP_QUEUE_SIZE", "20"),
		I18nDefaultLanguage:      GetEnv("I18N_DEFAULT_LANGUAGE", "en"),
		I18nLanguages:            configuredLanguages(GetEnv("I18N_LANGUAGES", ""), GetEnv("I18N_DEFAULT_LANGUAGE", "en")),
		sessionKey:               GetEnv("SESSION_KEY", "DefaultSessionKey_CHANGE_IT!"+hex.EncodeToString(b)),
		maxConcurrent:            GetEnvInt("CONCURRENCY_LIMIT", 100),
		MaxBodySize:              GetEnvBytes("MAX_BODY_SIZE", 1<<20),
		TZ:                       GetEnv("TZ", "Europe/Warsaw"),
		RateLimitSkipLocalhost:   GetEnvBool("RATE_LIMIT_SKIP_LOCALHOST", true),
		EnableGzip:               GetEnvBool("ENABLE_GZIP", true),
		RateLimiteSize:           GetEnvInt("RATE_LIMIT_SIZE", 10000),
		RateLimiteRate:           GetEnvInt("RATE_LIMIT_RATE", 360),
		RateLimiteWindow:         GetEnvDuration("RATE_LIMIT_WINDOW", time.Minute),
		JWTSecret:                GetEnv("JWT_SECRET", "DefaultJWTSecret_CHANGE_IT!"+hex.EncodeToString(b)),
		metricsToken:             GetEnv("METRICS_SECRET", "DefaultMetricToken_CHANGE_IT!"+hex.EncodeToString(b)),
		metricsEnabled:           GetEnvBool("METRICS_ENABLED", false),
	}
}

func configuredLanguages(value, defaultLanguage string) []string {
	languages := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	if len(languages) == 0 {
		return []string{defaultLanguage}
	}
	return languages
}

func (c Config) String() string {
	type configAlias Config

	aux := configAlias(c)

	for _, s := range []*string{
		&aux.WsTokenKey,
		&aux.sessionKey,
		&aux.PostgresPassword,
		&aux.SmtpPassword,
		&aux.JWTSecret,
		&aux.metricsToken,
		&aux.maintenanceBypassToken,
	} {
		if *s != "" {
			*s = "[REDACTED]"
		}
	}

	return formatConfiguredValues(aux)
}

func formatConfiguredValues(value any) string {
	return formatConfiguredValue(reflect.ValueOf(value))
}

func formatConfiguredValue(value reflect.Value) string {
	if !value.IsValid() || value.IsZero() {
		return ""
	}

	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if (value.Kind() == reflect.Map || value.Kind() == reflect.Slice) && value.Len() == 0 {
		return ""
	}

	switch value.Kind() {
	case reflect.Struct:
		fields := make([]string, 0, value.NumField())
		valueType := value.Type()
		for i := range value.NumField() {
			fieldName := valueType.Field(i).Name
			field := formatConfiguredField(fieldName, value.Field(i))
			if field == "" {
				continue
			}
			fields = append(fields, fieldName+":"+field)
		}
		if len(fields) == 0 {
			return ""
		}
		return "{" + strings.Join(fields, " ") + "}"
	case reflect.String:
		return value.String()
	case reflect.Bool:
		return strconv.FormatBool(value.Bool())
	case reflect.Int64:
		if value.Type() == reflect.TypeFor[time.Duration]() {
			return time.Duration(value.Int()).String()
		}
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(value.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'g', -1, value.Type().Bits())
	default:
		if value.CanInterface() {
			return fmt.Sprintf("%+v", value.Interface())
		}
		return fmt.Sprintf("%+v", value)
	}
}

func formatConfiguredField(name string, value reflect.Value) string {
	if (name == "MaxHeaderBytes" || name == "MaxBodySize") && value.IsValid() && !value.IsZero() {
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return formatByteSize(value.Int())
		}
	}
	return formatConfiguredValue(value)
}

func formatByteSize(size int64) string {
	for _, unit := range []struct {
		name  string
		bytes int64
	}{
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
	} {
		if size >= unit.bytes && size%unit.bytes == 0 {
			return strconv.FormatInt(size/unit.bytes, 10) + unit.name
		}
	}
	return strconv.FormatInt(size, 10) + "B"
}

var sensitiveRegex = regexp.MustCompile(`(?i)\b(email|phone|token|access_token|secret|password|key|auth)(["']?[:=]\s*["']?)([^"'\s,}\]]+)`)

type SanitizedWriter struct {
	Target io.Writer
}

func (sw SanitizedWriter) Write(p []byte) (n int, err error) {
	s := string(p)

	cleaned := sensitiveRegex.ReplaceAllStringFunc(s, func(match string) string {
		groups := sensitiveRegex.FindStringSubmatch(match)

		if len(groups) < 4 {
			return match
		}

		key := groups[1]
		sep := groups[2]
		val := groups[3]

		maskedVal := maskPartially(val)

		return key + sep + maskedVal
	})

	_, err = sw.Target.Write([]byte(cleaned))
	return len(p), err
}

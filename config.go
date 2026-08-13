package goserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"time"
)

type Config struct {
	APIPrefix              string
	WsTokenKey             string
	CsrfTrustedOrigins     string
	DatabaseUrl            string
	PostgresHost           string
	PostgresUser           string
	PostgresPassword       string
	PostgresDb             string
	DbExecMode             string
	DbMaxConns             string
	DbLogMode              string
	SmtpHost               string
	SmtpPort               string
	SmtpUser               string
	SmtpPassword           string
	SmtpFrom               string
	SmtpQueueSize          string
	SmtpWorkers            string
	sessionKey             string
	TZ                     string
	JWTSecret              string
	metricsToken           string
	MaxURLLength           int
	MaxHeaders             int
	MaxConnections         int64
	ReadTimeout            time.Duration
	WriteTimeout           time.Duration
	IdleTimeout            time.Duration
	ReadHeaderTimeout      time.Duration
	MaxHeaderBytes         int
	ShutdownTimeout        time.Duration
	PingTimeout            time.Duration
	WriteByteTimeout       time.Duration
	maxConcurrent          int
	MaxBodySize            int64
	RateLimiteSize         int
	RateLimiteRate         int
	RateLimiteWindow       time.Duration
	EnableSlowlorisCheck   bool
	RateLimitSkipLocalhost bool
	EnableGzip             bool
	metricsEnabled         bool
}

func (s *Server) loadConfig() {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		s.Logger.Fatal(err.Error())
	}

	s.Config = Config{
		APIPrefix:              GetEnv("API_PREFIX", ""),
		MaxURLLength:           GetEnvInt("MAX_URL_LENGTH", 2048),
		MaxHeaders:             GetEnvInt("MAX_HEADERS", 100),
		MaxConnections:         int64(GetEnvInt("MAX_CONNECTIONS", 10000)),
		ReadTimeout:            GetEnvDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:           GetEnvDuration("WRITE_TIMEOUT", 15*time.Second),
		WriteByteTimeout:       GetEnvDuration("WRITE_TIMEOUT", 5*time.Second),
		IdleTimeout:            GetEnvDuration("IDLE_TIMEOUT", 90*time.Second),
		PingTimeout:            GetEnvDuration("PING_TIMEOUT", 15*time.Second),
		ReadHeaderTimeout:      GetEnvDuration("READ_HEADER_TIMEOUT", 500*time.Millisecond),
		MaxHeaderBytes:         GetEnvInt("MAX_HEADER_BYTES", 1<<16),
		ShutdownTimeout:        GetEnvDuration("RELOAD_SHUTDOWN_TIMEOUT", 30*time.Second),
		EnableSlowlorisCheck:   GetEnv("ENABLE_SLOWLORIS_CHECK", "false") == "true",
		WsTokenKey:             GetEnv("WS_TOKEN_KEY", generateRandomKey()),
		CsrfTrustedOrigins:     GetEnv("CSRF_TRUSTED_ORIGINS", ""),
		DatabaseUrl:            GetEnv("DATABASE_URL", ""),
		PostgresHost:           GetEnv("POSTGRES_HOST", ""),
		PostgresUser:           GetEnv("POSTGRES_USER", "postgres"),
		PostgresPassword:       GetEnv("POSTGRES_PASSWORD", ""),
		PostgresDb:             GetEnv("POSTGRES_DB", "postgres"),
		DbExecMode:             GetEnv("DB_EXEC_MODE", ""),
		DbMaxConns:             GetEnv("DB_MAX_CONNS", ""),
		DbLogMode:              GetEnv("DB_LOG_MODE", "sanitized"),
		SmtpHost:               GetEnv("SMTP_HOST", ""),
		SmtpPort:               GetEnv("SMTP_PORT", ""),
		SmtpUser:               GetEnv("SMTP_USER", ""),
		SmtpPassword:           GetEnv("SMTP_PASS", ""),
		SmtpFrom:               GetEnv("SMTP_FROM", ""),
		SmtpWorkers:            GetEnv("SMTP_FROM", "1"),
		SmtpQueueSize:          GetEnv("SMTP_QUEUE_SIZE", "20"),
		sessionKey:             GetEnv("SESSION_KEY", "DefaultSessionKey_CHANGE_IT!"+hex.EncodeToString(b)),
		maxConcurrent:          GetEnvInt("CONCURRENCY_LIMIT", 100),
		MaxBodySize:            GetEnvBytes("MAX_BODY_SIZE", 1<<20),
		TZ:                     GetEnv("TZ", "Europe/Warsaw"),
		RateLimitSkipLocalhost: GetEnvBool("RATE_LIMIT_SKIP_LOCALHOST", true),
		EnableGzip:             GetEnvBool("ENABLE_GZIP", true),
		RateLimiteSize:         GetEnvInt("RATE_LIMIT_SIZE", 10000),
		RateLimiteRate:         GetEnvInt("RATE_LIMIT_RATE", 360),
		RateLimiteWindow:       GetEnvDuration("RATE_LIMIT_WINDOW", time.Minute),
		JWTSecret:              GetEnv("JWT_SECRET", "DefaultJWTSecret_CHANGE_IT!"+hex.EncodeToString(b)),
		metricsToken:           GetEnv("METRICS_SECRET", "DefaultMetricToken_CHANGE_IT!"+hex.EncodeToString(b)),
		metricsEnabled:         GetEnvBool("METRICS_ENABLED", false),
	}
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
	} {
		if *s != "" {
			*s = "[REDACTED]"
		}
	}

	return fmt.Sprintf("%+v", aux)
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

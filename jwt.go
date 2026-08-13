package goserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const jwtHeaderHS256 = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

var (
	ErrTokenMalformed = errors.New("invalid token format")
	ErrTokenSignature = errors.New("invalid token signature")
	ErrTokenExpired   = errors.New("token has expired")
)

type JWTManager struct {
	secret []byte
}

func NewJWTManager(secretKey string) *JWTManager {
	if secretKey == "" {
		panic("JWT secret key cannot be empty")
	}
	return &JWTManager{
		secret: []byte(secretKey),
	}
}

type jwtPayload[T any] struct {
	Data T     `json:"data"`
	Exp  int64 `json:"exp"`
}

func GenerateToken[T any](m *JWTManager, data T, ttl time.Duration) (string, error) {
	payload := jwtPayload[T]{
		Exp:  time.Now().Add(ttl).Unix(),
		Data: data,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	unsignedToken := jwtHeaderHS256 + "." + encodedPayload

	signature := m.sign(unsignedToken)

	return unsignedToken + "." + signature, nil
}

func ValidateToken[T any](m *JWTManager, tokenString string) (T, error) {
	var zero T

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return zero, ErrTokenMalformed
	}

	unsignedToken := parts[0] + "." + parts[1]
	providedSignature := parts[2]

	expectedSignature := m.sign(unsignedToken)

	if !hmac.Equal([]byte(providedSignature), []byte(expectedSignature)) {
		return zero, ErrTokenSignature
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, ErrTokenMalformed
	}

	var payload jwtPayload[T]
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return zero, err
	}

	if time.Now().Unix() > payload.Exp {
		return zero, ErrTokenExpired
	}

	return payload.Data, nil
}

func (m *JWTManager) sign(data string) string {
	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

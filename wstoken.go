package goserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func GenerateWSToken(secretKey string, duration time.Duration) string {
	exp := time.Now().Add(duration).Unix()
	payload := fmt.Sprintf("%d", exp)

	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(payload))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	return fmt.Sprintf("%s.%s", payload, signature)
}

func ValidateWSToken(tokenString, secretKey string) error {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 2 {
		return errors.New("invalid token format")
	}

	payloadStr := parts[0]
	signature := parts[1]

	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(payloadStr))
	expectedSig := base64.URLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return errors.New("invalid signature")
	}

	exp, err := strconv.ParseInt(payloadStr, 10, 64)
	if err != nil {
		return errors.New("invalid timestamp")
	}

	if time.Now().Unix() > exp {
		return errors.New("token expired")
	}

	return nil
}

func (s *Server) authenticateWsRequest(r *http.Request, secretKey string) error {
	token := r.URL.Query().Get("token")
	if token == "" {
		return errors.New("missing token")
	}

	if err := ValidateWSToken(token, secretKey); err != nil {
		return err
	}

	return nil
}

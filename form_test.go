package goserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBrowserFormIntegerNormalization(t *testing.T) {
	s := NewServer(":0")
	form := url.Values{
		"age":   {"37"},
		"email": {"ada@example.com"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	var input *ParsedRequest
	var parseErr error
	handler := s.ServerContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		input, parseErr = ParseRequest(r)
	}))
	handler.ServeHTTP(recorder, req)

	if parseErr != nil {
		t.Fatalf("ParseRequest returned an error: %v", parseErr)
	}
	if input == nil {
		t.Fatal("ParseRequest returned nil input")
	}

	validationErrors := input.Validate([]ValidationRule{
		{Name: "age", Type: "int", Required: true, Min: FloatPtr(18)},
	})
	if len(validationErrors) != 0 {
		t.Fatalf("browser form validation failed: %v", validationErrors)
	}

	age, ok := input.GetInt("age")
	if !ok {
		t.Fatal("validated browser form age was not normalized to int")
	}
	if age != 37 {
		t.Fatalf("age = %d, want 37", age)
	}
	if _, ok := input.Fields["age"].(int); !ok {
		t.Fatalf("Fields[age] has type %T, want int", input.Fields["age"])
	}
}

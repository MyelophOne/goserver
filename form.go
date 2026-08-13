package goserver

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type ParsedRequest struct {
	Fields map[string]any
	Files  map[string][]*multipart.FileHeader
	Raw    []byte
}

type ValidationRule struct {
	Default      any
	MinLen       *int
	MaxLen       *int
	Regex        *regexp.Regexp
	Min          *float64
	Max          *float64
	Name         string
	Type         string
	CustomMsg    string
	ElemType     string
	Enum         []any
	Nested       []ValidationRule
	AllowedMime  []string
	MaxFileBytes int64
	Required     bool
}

type ValidationError struct {
	Field   string
	Message string
}

func (ve ValidationError) Error() string {
	return fmt.Sprintf("field '%s': %s", ve.Field, ve.Message)
}

var (
	RegexEmail = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	RegexPhone = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
	RegexURL   = regexp.MustCompile(`^(https?://)?([\da-z\.-]+)\.([a-z\.]{2,6})([/\w \.-]*)/?$`)
	RegexUUID  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

func ParseRequest(r *http.Request) (*ParsedRequest, error) {
	s := GetServer(r.Context())
	if s == nil {
		return nil, nil
	}

	MaxBodySize := s.Config.MaxBodySize

	result := &ParsedRequest{
		Fields: make(map[string]any),
		Files:  make(map[string][]*multipart.FileHeader),
	}

	ct := r.Header.Get("Content-Type")

	switch {
	case strings.HasPrefix(ct, "application/json"):
		r.Body = http.MaxBytesReader(nil, r.Body, MaxBodySize)
		defer func() {
			_ = r.Body.Close()
		}()
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&result.Fields); err != nil {
			if err == io.EOF {
				return result, nil
			}
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}

	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		r.Body = http.MaxBytesReader(nil, r.Body, MaxBodySize)
		if err := r.ParseForm(); err != nil {
			return nil, fmt.Errorf("cannot parse form: %w", err)
		}
		for k, v := range r.Form {
			if len(v) == 1 {
				result.Fields[k] = v[0]
			} else {
				result.Fields[k] = v
			}
		}

	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, fmt.Errorf("cannot parse multipart form: %w", err)
		}
		for k, v := range r.MultipartForm.Value {
			if len(v) == 1 {
				result.Fields[k] = v[0]
			} else {
				result.Fields[k] = v
			}
		}
		maps.Copy(result.Files, r.MultipartForm.File)

	default:
		r.Body = http.MaxBytesReader(nil, r.Body, MaxBodySize)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		result.Raw = body
	}

	return result, nil
}

func (pr *ParsedRequest) Validate(rules []ValidationRule) []error {
	return validateRecursive(rules, pr.Fields, pr.Files, "")
}

func validateRecursive(rules []ValidationRule, fields map[string]any, files map[string][]*multipart.FileHeader, prefix string) []error {
	var errs []error

	addErr := func(rule ValidationRule, msg string) {
		if rule.CustomMsg != "" {
			msg = rule.CustomMsg
		}
		fullPath := rule.Name
		if prefix != "" {
			fullPath = prefix + "." + rule.Name
		}
		errs = append(errs, ValidationError{Field: fullPath, Message: msg})
	}

	for _, rule := range rules {
		if rule.Type == "file" {
			fileList, exists := files[rule.Name]
			if !exists || len(fileList) == 0 {
				if rule.Required {
					addErr(rule, "file is required")
				}
				continue
			}
			for _, fh := range fileList {
				if rule.MaxFileBytes > 0 && fh.Size > rule.MaxFileBytes {
					addErr(rule, fmt.Sprintf("file exceeds maximum size of %d bytes", rule.MaxFileBytes))
				}
				if len(rule.AllowedMime) > 0 {
					mime := fh.Header.Get("Content-Type")
					if !containsString(rule.AllowedMime, mime) {
						addErr(rule, fmt.Sprintf("unsupported file type: %s", mime))
					}
				}
			}
			continue
		}

		val, exists := fields[rule.Name]

		if !exists || val == nil || val == "" {
			switch {
			case rule.Default != nil:
				fields[rule.Name] = rule.Default
				val = rule.Default
			case rule.Required:
				addErr(rule, "required")
				continue
			default:
				continue
			}
		}

		switch rule.Type {
		case "string":
			s := fmt.Sprintf("%v", val)
			fields[rule.Name] = s

			if rule.MinLen != nil && len(s) < *rule.MinLen {
				addErr(rule, fmt.Sprintf("min length is %d", *rule.MinLen))
			}
			if rule.MaxLen != nil && len(s) > *rule.MaxLen {
				addErr(rule, fmt.Sprintf("max length is %d", *rule.MaxLen))
			}
			if len(rule.Enum) > 0 && !containsStringAny(rule.Enum, s) {
				addErr(rule, "value not allowed")
			}
			if rule.Regex != nil && !rule.Regex.MatchString(s) {
				addErr(rule, "does not match pattern")
			}

		case "int":
			i, ok := toInt(val)
			if !ok {
				addErr(rule, "must be an integer")
				continue
			}
			fields[rule.Name] = i

			if rule.Min != nil && float64(i) < *rule.Min {
				addErr(rule, fmt.Sprintf("minimum is %v", *rule.Min))
			}
			if rule.Max != nil && float64(i) > *rule.Max {
				addErr(rule, fmt.Sprintf("maximum is %v", *rule.Max))
			}
			if len(rule.Enum) > 0 && !containsNumber(rule.Enum, float64(i)) {
				addErr(rule, "value not allowed")
			}

		case "float":
			f, ok := toFloat(val)
			if !ok {
				addErr(rule, "must be a float")
				continue
			}
			fields[rule.Name] = f

			if rule.Min != nil && f < *rule.Min {
				addErr(rule, fmt.Sprintf("minimum is %v", *rule.Min))
			}
			if rule.Max != nil && f > *rule.Max {
				addErr(rule, fmt.Sprintf("maximum is %v", *rule.Max))
			}

		case "bool":
			b, ok := val.(bool)
			if !ok {
				if s, ok := val.(string); ok {
					parsedBool, err := strconv.ParseBool(s)
					if err == nil {
						fields[rule.Name] = parsedBool
						continue
					}
				}
				addErr(rule, "must be a boolean")
			} else {
				fields[rule.Name] = b
			}

		case "object":
			nestedMap, ok := val.(map[string]any)
			if !ok {
				addErr(rule, "must be an object")
				continue
			}
			newPrefix := rule.Name
			if prefix != "" {
				newPrefix = prefix + "." + rule.Name
			}
			nestedErrs := validateRecursive(rule.Nested, nestedMap, files, newPrefix)
			errs = append(errs, nestedErrs...)

		case "slice":
			sliceAny, ok := val.([]any)
			if !ok {
				if sliceStr, okStr := val.([]string); okStr {
					for _, item := range sliceStr {
						sliceAny = append(sliceAny, item)
					}
				} else {
					addErr(rule, "must be an array/slice")
					continue
				}
			}

			for idx, item := range sliceAny {
				itemPath := fmt.Sprintf("%s[%d]", rule.Name, idx)
				if prefix != "" {
					itemPath = fmt.Sprintf("%s.%s[%d]", prefix, rule.Name, idx)
				}

				switch rule.ElemType {
				case "int":
					if _, ok := toInt(item); !ok {
						errs = append(errs, ValidationError{Field: itemPath, Message: "element must be int"})
					}
				case "float":
					if _, ok := toFloat(item); !ok {
						errs = append(errs, ValidationError{Field: itemPath, Message: "element must be float"})
					}
				case "string":
					if _, ok := item.(string); !ok {
						errs = append(errs, ValidationError{Field: itemPath, Message: "element must be string"})
					}
				}
			}
		}
	}

	return errs
}

func (pr *ParsedRequest) GetString(key string) string {
	if val, ok := pr.Fields[key]; ok {
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func (pr *ParsedRequest) GetInt(key string) (int, bool) {
	if val, ok := pr.Fields[key]; ok {
		return toInt(val)
	}
	return 0, false
}

func (pr *ParsedRequest) GetFloat(key string) (float64, bool) {
	if val, ok := pr.Fields[key]; ok {
		return toFloat(val)
	}
	return 0, false
}

func (pr *ParsedRequest) GetBool(key string) (bool, bool) {
	if val, ok := pr.Fields[key]; ok {
		if b, ok := val.(bool); ok {
			return b, true
		}
	}
	return false, false
}

func (pr *ParsedRequest) GetSlice(key string) ([]any, bool) {
	if val, ok := pr.Fields[key]; ok {
		if sl, ok := val.([]any); ok {
			return sl, true
		}
	}
	return nil, false
}

func (pr *ParsedRequest) GetMap(key string) (map[string]any, bool) {
	if val, ok := pr.Fields[key]; ok {
		if m, ok := val.(map[string]any); ok {
			return m, true
		}
	}
	return nil, false
}

func (pr *ParsedRequest) GetFiles(key string) []*multipart.FileHeader {
	return pr.Files[key]
}

func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		i, err := strconv.Atoi(v)
		return i, err == nil
	}
	return 0, false
}

func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func containsString(arr []string, target string) bool {
	for _, a := range arr {
		if a == target {
			return true
		}
	}
	return false
}

func containsStringAny(arr []any, target string) bool {
	for _, a := range arr {
		if s, ok := a.(string); ok && s == target {
			return true
		}
	}
	return false
}

func containsNumber(arr []any, target float64) bool {
	for _, a := range arr {
		if f, ok := toFloat(a); ok && f == target {
			return true
		}
	}
	return false
}

func IntPtr(i int) *int           { return &i }
func FloatPtr(f float64) *float64 { return &f }

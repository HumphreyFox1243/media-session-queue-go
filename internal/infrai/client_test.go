package infrai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateUserDecodesBusinessErrorBeforeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/user/create" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"INVALID_ARGUMENT","message":"email is required"},"metadata":{}}`))
	}))
	defer server.Close()

	client := NewWithBaseURL("test-key", server.URL, server.Client())
	_, err := client.CreateUser(context.Background(), CreateUserInput{IdempotencyKey: "req-1"})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != "INVALID_ARGUMENT" || apiErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("error = %#v", err)
	}
}

func TestCaptchaRetriesRateLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["widget_record_id"] != "widget-001" {
			t.Errorf("captcha request = %v, decode error = %v", input, err)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"RATE_LIMITED","message":"retry later"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"valid":true,"score":0.91},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client := NewWithBaseURL("test-key", server.URL, server.Client())
	client.sleep = func(context.Context, time.Duration) error { return nil }
	result, err := client.VerifyCaptcha(context.Background(), CaptchaInput{WidgetRecordID: "widget-001", Token: "proof"})
	if err != nil || !result.Valid || calls != 2 {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
	}
}

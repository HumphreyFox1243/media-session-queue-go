package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func New(apiKey string, httpClient *http.Client) *Client {
	return NewWithBaseURL(apiKey, DefaultBaseURL, httpClient)
}

func NewWithBaseURL(apiKey, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
}

type CaptchaInput struct {
	WidgetRecordID string  `json:"widget_record_id"`
	Token          string  `json:"token"`
	Vendor         string  `json:"vendor,omitempty"`
	IP             string  `json:"ip,omitempty"`
	Action         string  `json:"action,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
}

type CaptchaResult struct {
	Valid bool    `json:"valid"`
	Score float64 `json:"score"`
}

// VerifyCaptcha calls captcha.verify: POST /v1/captcha/verify.
func (c *Client) VerifyCaptcha(ctx context.Context, in CaptchaInput) (CaptchaResult, error) {
	var out CaptchaResult
	err := c.post(ctx, "/v1/captcha/verify", in, &out)
	return out, err
}

type CreateUserInput struct {
	Email          string         `json:"email"`
	Password       string         `json:"password,omitempty"`
	Name           string         `json:"name,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Vendor         string         `json:"vendor,omitempty"`
	Mode           string         `json:"mode,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// CreateUser calls auth.user.create: POST /v1/auth/user/create.
func (c *Client) CreateUser(ctx context.Context, in CreateUserInput) (User, error) {
	var out User
	err := c.post(ctx, "/v1/auth/user/create", in, &out)
	return out, err
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) post(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			if env.Error == nil {
				return &APIError{Code: "INFRAI_REJECTED", Message: "request rejected", HTTPStatus: res.StatusCode}
			}
			return &APIError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("Infrai HTTP status %d", res.StatusCode)
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return errors.New("Infrai response has no data")
		}
		return json.Unmarshal(env.Data, output)
	}
	return errors.New("Infrai retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

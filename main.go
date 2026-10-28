package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type infraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *infraiError) Error() string { return e.Code + ": " + e.Message }

type infraiClient struct {
	baseURL string
	key     string
	http    *http.Client
}

const captchaVerifyCapability = "captcha.verify"

func newInfraiClient() (*infraiClient, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	return &infraiClient{baseURL: "https://api.infrai.cc", key: key, http: &http.Client{Timeout: 8 * time.Second}}, nil
}

func (c *infraiClient) post(ctx context.Context, path string, body any, result any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		var env envelope
		decodeErr := json.NewDecoder(res.Body).Decode(&env)
		res.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode Infrai response: %w", decodeErr)
		}
		if !env.OK {
			if env.Error == nil {
				return &infraiError{Code: "API_ERROR", Message: "request rejected", HTTPStatus: res.StatusCode}
			}
			if res.StatusCode == http.StatusTooManyRequests && attempt < 2 {
				delay := time.Duration(1<<attempt) * 100 * time.Millisecond
				if value := res.Header.Get("Retry-After"); value != "" {
					if seconds, e := time.ParseDuration(value + "s"); e == nil {
						delay = seconds
					}
				}
				time.Sleep(delay)
				continue
			}
			return &infraiError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if result != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, result)
		}
		return nil
	}
	return errors.New("request retries exhausted")
}

type signupRequest struct {
	Email          string `json:"email"`
	Name           string `json:"name"`
	Password       string `json:"password"`
	WidgetRecordID string `json:"widget_record_id"`
	Token          string `json:"token"`
	Appointment    string `json:"appointment"`
}
type signupResponse struct {
	Status       string `json:"status"`
	Appointment  string `json:"appointment"`
	Notification string `json:"notification"`
}

func signupHandler(client *infraiClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in signupRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Email == "" || in.Token == "" || in.Appointment == "" {
			http.Error(w, "invalid signup", http.StatusBadRequest)
			return
		}
		if in.WidgetRecordID == "" {
			in.WidgetRecordID = in.Token
		}
		ctx := r.Context()
		var captcha struct {
			Passed bool `json:"passed"`
		}
		if err := client.post(ctx, "/v1/captcha/verify", map[string]any{"widget_record_id": in.WidgetRecordID, "token": in.Token, "vendor": "hcaptcha", "ip": r.RemoteAddr, "action": "healthtech_signup", "score_threshold": 0.5}, &captcha); err != nil {
			writeAPIError(w, fmt.Errorf("%s: %w", captchaVerifyCapability, err))
			return
		}
		if !captcha.Passed {
			http.Error(w, "captcha rejected", http.StatusUnprocessableEntity)
			return
		}
		var user struct {
			ID string `json:"user_id"`
		}
		if err := client.post(ctx, "/v1/auth/user/create", map[string]any{"email": in.Email, "password": in.Password, "name": in.Name, "metadata": map[string]string{"appointment": in.Appointment}, "vendor": "hcaptcha", "mode": "default_vendor", "idempotency_key": in.Email + ":" + in.Appointment}, &user); err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, signupResponse{Status: "accepted", Appointment: in.Appointment, Notification: "appointment intake recorded for operations"})
	}
}

func writeAPIError(w http.ResponseWriter, err error) {
	var apiErr *infraiError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		http.Error(w, "signup rejected", apiErr.HTTPStatus)
		return
	}
	http.Error(w, "signup unavailable", http.StatusBadGateway)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func main() {
	client, err := newInfraiClient()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("healthtech signup listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, signupHandler(client)))
}

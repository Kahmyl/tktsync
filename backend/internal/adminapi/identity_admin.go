package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	errIdentityAdminUnavailable = errors.New("identity administration is unavailable")
	errIdentityInviteRejected   = errors.New("identity invitation was rejected")
)

type IdentityUser struct {
	ID    uuid.UUID
	Email string
}

type IdentityAdmin interface {
	EnsureInvited(context.Context, string, string) (IdentityUser, bool, error)
}

type IdentityAdminFunc func(context.Context, string, string) (IdentityUser, bool, error)

func (fn IdentityAdminFunc) EnsureInvited(ctx context.Context, email, displayName string) (IdentityUser, bool, error) {
	return fn(ctx, email, displayName)
}

type supabaseIdentityAdmin struct {
	baseURL     string
	secretKey   string
	redirectURL string
	client      *http.Client
}

func ValidateProductionIdentityAdmin(baseURL, secretKey, redirectURL string) error {
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("SUPABASE_URL is required for administrator invitations in production")
	}
	if strings.TrimSpace(secretKey) == "" {
		return errors.New("SUPABASE_SECRET_KEY is required for administrator invitations in production")
	}
	for name, raw := range map[string]string{
		"SUPABASE_URL":              baseURL,
		"ADMIN_INVITE_REDIRECT_URL": redirectURL,
	} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%s must be an absolute HTTPS URL in production", name)
		}
	}
	return nil
}

func NewSupabaseIdentityAdmin(baseURL, secretKey, redirectURL string) (IdentityAdmin, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	secretKey = strings.TrimSpace(secretKey)
	redirectURL = strings.TrimSpace(redirectURL)
	if secretKey == "" {
		return nil, nil
	}
	if baseURL == "" {
		return nil, errors.New("Supabase identity administration requires SUPABASE_URL and SUPABASE_SECRET_KEY")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, errors.New("SUPABASE_URL must be an absolute HTTP(S) URL")
	}
	if redirectURL != "" {
		redirect, parseErr := url.Parse(redirectURL)
		if parseErr != nil || (redirect.Scheme != "https" && redirect.Scheme != "http") || redirect.Host == "" {
			return nil, errors.New("ADMIN_INVITE_REDIRECT_URL must be an absolute HTTP(S) URL")
		}
	}
	return &supabaseIdentityAdmin{
		baseURL: baseURL, secretKey: secretKey, redirectURL: redirectURL,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (s *supabaseIdentityAdmin) EnsureInvited(ctx context.Context, email, displayName string) (IdentityUser, bool, error) {
	user, found, err := s.findByEmail(ctx, email)
	if err != nil {
		return IdentityUser{}, false, err
	}
	if found {
		return user, false, nil
	}

	body, err := json.Marshal(map[string]any{
		"email": email,
		"data": map[string]any{
			"display_name":                    displayName,
			"name":                            displayName,
			"tktsync_password_setup_required": true,
		},
	})
	if err != nil {
		return IdentityUser{}, false, err
	}
	endpoint := s.baseURL + "/auth/v1/invite"
	if s.redirectURL != "" {
		endpoint += "?redirect_to=" + url.QueryEscape(s.redirectURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return IdentityUser{}, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	s.authorize(request)
	response, err := s.client.Do(request)
	if err != nil {
		return IdentityUser{}, false, fmt.Errorf("%w: %v", errIdentityAdminUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		// A concurrent request may have created the identity after our initial lookup.
		if response.StatusCode == http.StatusUnprocessableEntity || response.StatusCode == http.StatusBadRequest {
			if existing, exists, findErr := s.findByEmail(ctx, email); findErr == nil && exists {
				return existing, false, nil
			}
			return IdentityUser{}, false, errIdentityInviteRejected
		}
		return IdentityUser{}, false, errIdentityAdminUnavailable
	}
	user, err = decodeIdentityUser(response.Body)
	if err != nil {
		return IdentityUser{}, false, fmt.Errorf("%w: invalid invite response", errIdentityAdminUnavailable)
	}
	return user, true, nil
}

func (s *supabaseIdentityAdmin) findByEmail(ctx context.Context, email string) (IdentityUser, bool, error) {
	for page := 1; page <= 100; page++ {
		endpoint := fmt.Sprintf("%s/auth/v1/admin/users?page=%d&per_page=100", s.baseURL, page)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return IdentityUser{}, false, err
		}
		s.authorize(request)
		response, err := s.client.Do(request)
		if err != nil {
			return IdentityUser{}, false, fmt.Errorf("%w: %v", errIdentityAdminUnavailable, err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return IdentityUser{}, false, errIdentityAdminUnavailable
		}
		var result struct {
			Users []struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"users"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result)
		response.Body.Close()
		if decodeErr != nil {
			return IdentityUser{}, false, fmt.Errorf("%w: invalid user list response", errIdentityAdminUnavailable)
		}
		for _, candidate := range result.Users {
			if strings.EqualFold(strings.TrimSpace(candidate.Email), email) {
				id, parseErr := uuid.Parse(candidate.ID)
				if parseErr != nil {
					return IdentityUser{}, false, fmt.Errorf("%w: invalid user identity", errIdentityAdminUnavailable)
				}
				return IdentityUser{ID: id, Email: strings.ToLower(strings.TrimSpace(candidate.Email))}, true, nil
			}
		}
		if len(result.Users) < 100 {
			return IdentityUser{}, false, nil
		}
	}
	return IdentityUser{}, false, errIdentityAdminUnavailable
}

func (s *supabaseIdentityAdmin) authorize(request *http.Request) {
	request.Header.Set("apikey", s.secretKey)
	request.Header.Set("Authorization", "Bearer "+s.secretKey)
}

func decodeIdentityUser(reader io.Reader) (IdentityUser, error) {
	var payload struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&payload); err != nil {
		return IdentityUser{}, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil || strings.TrimSpace(payload.Email) == "" {
		return IdentityUser{}, errors.New("identity response is incomplete")
	}
	return IdentityUser{ID: id, Email: strings.ToLower(strings.TrimSpace(payload.Email))}, nil
}

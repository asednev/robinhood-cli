package rhx

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

// PortfolioConnection owns a profile's local brokerage session. It exposes no
// trading methods. Calls for the same profile serialize across processes.
type PortfolioConnection struct{ auth *AuthManager }

// PortfolioCredentials are supplied for explicit login only and are never saved.
type PortfolioCredentials struct{ Username, Password string }

var portfolioProfilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// NewPortfolioConnection selects a host-local session directory and profile.
// An empty directory uses rhx's normal session directory. No keyring is used.
func NewPortfolioConnection(profile, sessionDir string) (*PortfolioConnection, error) {
	if profile == "" {
		profile = "default"
	}
	if !portfolioProfilePattern.MatchString(profile) {
		return nil, newError(ErrorValidation, "Invalid profile")
	}
	if sessionDir == "" {
		sessionDir = defaultSessionDir()
	}
	if !filepath.IsAbs(sessionDir) {
		return nil, newError(ErrorValidation, "Session directory must be absolute")
	}
	client := newHTTPClient(nil)
	client.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &PortfolioConnection{auth: &AuthManager{Profile: profile, SessionPath: sessionPath(sessionDir, profile), Client: client}}, nil
}

// Login authenticates with supplied credentials. waitForChallenge allows the
// existing interactive Robinhood challenge flow; routine refresh never does.
func (c *PortfolioConnection) Login(ctx context.Context, credentials PortfolioCredentials, waitForChallenge bool) error {
	if strings.TrimSpace(credentials.Username) == "" || strings.TrimSpace(credentials.Password) == "" {
		return newError(ErrorValidation, "Credentials are required")
	}
	return c.withSessionLock(ctx, func() error {
		c.auth.Client.clearSession()
		session, err := c.auth.login(ctx, credentials.Username, credentials.Password, waitForChallenge)
		if err != nil {
			return portfolioAuthError(err)
		}
		if err := saveSession(c.auth.SessionPath, session); err != nil {
			return newError(ErrorAuthRequired, "Could not store brokerage session")
		}
		c.auth.Client.setSession(session)
		return nil
	})
}

// RefreshSession verifies or renews a stored session without password login,
// keyring access, prompts, or challenge polling.
func (c *PortfolioConnection) RefreshSession(ctx context.Context) error {
	return c.withSessionLock(ctx, func() error { return c.refreshSessionLocked(ctx) })
}

func (c *PortfolioConnection) refreshSessionLocked(ctx context.Context) error {
	session, err := loadSession(c.auth.SessionPath)
	if err != nil || session.AccessToken == "" {
		return newError(ErrorAuthRequired, "Brokerage authentication required")
	}
	c.auth.Client.setSession(session)
	data, status, err := c.auth.Client.getRaw(ctx, robinhoodAPIBase+"/positions/", map[string]string{"nonzero": "true"})
	if err != nil {
		return portfolioAuthError(err)
	}
	if status == http.StatusOK {
		if _, ok := asMap(data)["results"].([]any); !ok {
			return newError(ErrorBrokerRejected, "Brokerage authentication unavailable")
		}
		return nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return newError(ErrorBrokerRejected, "Brokerage authentication unavailable")
	}
	if session.RefreshToken == "" {
		return newError(ErrorAuthRequired, "Brokerage authentication required")
	}
	c.auth.Client.clearSession()
	refreshed, err := c.auth.refreshSession(ctx, session, false)
	if err != nil {
		return portfolioAuthError(err)
	}
	if err := saveSession(c.auth.SessionPath, refreshed); err != nil {
		return newError(ErrorAuthRequired, "Could not store brokerage session")
	}
	c.auth.Client.setSession(refreshed)
	return nil
}

func portfolioAuthError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code := cliError(err).Code
	if code == ErrorMFARequired || code == ErrorAuthRequired {
		return newError(ErrorAuthRequired, "Brokerage authentication required")
	}
	return newError(ErrorBrokerRejected, "Brokerage authentication unavailable")
}

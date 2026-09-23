package openidauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"main/internal/requestlog"

	"golang.org/x/net/context"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)




func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}


const (
	pendingAuthTransactionTTL  = 10 * time.Minute
	maxPendingAuthTransactions = 128
)

type pendingAuthTransaction struct {
	nonce          string
	redirectTo     string
	sessionBinding string
	expiresAt      time.Time
}

type pendingAuthTransactions struct {
	mu           sync.Mutex
	transactions map[string]pendingAuthTransaction
}

func newPendingAuthTransactions() *pendingAuthTransactions {
	return &pendingAuthTransactions{transactions: make(map[string]pendingAuthTransaction)}
}

func (transactions *pendingAuthTransactions) add(state string, transaction pendingAuthTransaction) bool {
	transactions.mu.Lock()
	defer transactions.mu.Unlock()

	now := time.Now()
	for state, transaction := range transactions.transactions {
		if !transaction.expiresAt.After(now) {
			delete(transactions.transactions, state)
		}
	}

	if len(transactions.transactions) >= maxPendingAuthTransactions {
		return false
	}

	transactions.transactions[state] = transaction
	return true
}

func (transactions *pendingAuthTransactions) take(state string) (pendingAuthTransaction, bool) {
	transactions.mu.Lock()
	defer transactions.mu.Unlock()

	transaction, ok := transactions.transactions[state]
	if !ok || !transaction.expiresAt.After(time.Now()) {
		delete(transactions.transactions, state)
		return pendingAuthTransaction{}, false
	}

	delete(transactions.transactions, state)
	return transaction, true
}
type AuthHandler struct {
	context		*context.Context
	provider	*oidc.Provider
	verifier	*oidc.IDTokenVerifier
	oauth2Conf  *oauth2.Config
	expirationTimer	int64
	session_label_expired string
	session_label_userid string
	session_label_login_binding string
	default_authenticated_url string
	pending_auth_transactions *pendingAuthTransactions
	logger *slog.Logger
}
func (a AuthHandler) UserIDLabel() string {
	return a.session_label_userid
}
func NewAuthHandler(clientID string, clientSecret string, sessionExpiration int64, issuerUrl string, redirectURL string, configuredLogger ...*slog.Logger) AuthHandler{
	logger := slog.Default()
	if len(configuredLogger) > 0 && configuredLogger[0] != nil {
		logger = configuredLogger[0]
	}
	logger = logger.With("component", "oidc")
	context := context.Background()

	provider, err := oidc.NewProvider(context, issuerUrl)
	if err != nil {
		logger.Error("OIDC provider initialization failed", "event", "oidc.provider.initialization_failed", "error", err)
		panic(err)
	}
	if sessionExpiration <= 0 {
		logger.Error("OIDC session expiration is invalid", "event", "oidc.configuration_invalid")
		panic(errors.New("Invalid AuthHandler session expiration!"))
	}
	oidcConfig := &oidc.Config{
		ClientID: clientID,
	}
	verifier := provider.Verifier(oidcConfig)
	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		// Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Scopes:       []string{oidc.ScopeOpenID, "sub", "email"},
	}
	return AuthHandler{provider: provider, verifier: verifier, oauth2Conf: &config, context: &context,
		expirationTimer: sessionExpiration,
		session_label_expired: "expiration", session_label_userid: "sub", session_label_login_binding: "auth_login_binding",
		default_authenticated_url: "/tray/", pending_auth_transactions: newPendingAuthTransactions(), logger: logger }
}

func (handler *AuthHandler) requestLogger(ctx *gin.Context) *slog.Logger {
	return requestlog.FromGinOr(ctx, handler.logger).With("component", "oidc")
}

func (handler *AuthHandler) Login() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		handler.startLogin(ctx, handler.default_authenticated_url)
	}
}

func (handler *AuthHandler) startLogin(ctx *gin.Context, redirectTo string) {
	logger := handler.requestLogger(ctx)
	if !isLocalRedirect(redirectTo) {
		redirectTo = handler.default_authenticated_url
	}

	w := ctx.Writer
	lstate, err := randString(16)
	if err != nil {
		logger.Error("OIDC login setup failed", "event", "oidc.login.failed", "stage", "state", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		ctx.Abort()
		return
	}
	nonce, err := randString(16)
	if err != nil {
		logger.Error("OIDC login setup failed", "event", "oidc.login.failed", "stage", "nonce", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		ctx.Abort()
		return
	}
	session := sessions.Default(ctx)
	loginBinding, ok := session.Get(handler.session_label_login_binding).(string)
	if !ok || loginBinding == "" {
		loginBinding, err = randString(16)
		if err != nil {
			logger.Error("OIDC login setup failed", "event", "oidc.login.failed", "stage", "session_binding", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			ctx.Abort()
			return
		}
		session.Set(handler.session_label_login_binding, loginBinding)
		if err := session.Save(); err != nil {
			logger.Error("OIDC login setup failed", "event", "oidc.login.failed", "stage", "session_save", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			ctx.Abort()
			return
		}
	}

	if !handler.pending_auth_transactions.add(lstate, pendingAuthTransaction{
		nonce:          nonce,
		redirectTo:     redirectTo,
		sessionBinding: loginBinding,
		expiresAt:      time.Now().Add(pendingAuthTransactionTTL),
	}) {
		logger.Warn("OIDC login rejected", "event", "oidc.login.rejected", "reason", "too_many_pending_transactions")
		http.Error(w, "too many sign-in requests are in progress; try again shortly", http.StatusServiceUnavailable)
		ctx.Abort()
		return
	}

	ctx.Redirect(http.StatusFound, handler.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)))
	ctx.Abort()
}

func isLocalRedirect(redirectTo string) bool {
	parsedURL, err := url.Parse(redirectTo)
	return err == nil &&
		!parsedURL.IsAbs() &&
		parsedURL.Host == "" &&
		strings.HasPrefix(parsedURL.Path, "/") &&
		!strings.HasPrefix(redirectTo, "//") &&
		!strings.Contains(parsedURL.Path, "\\")
}

func (handler *AuthHandler) Logout() gin.HandlerFunc{
	return func (ctx *gin.Context) {
		logger := handler.requestLogger(ctx)
		s := sessions.Default(ctx)
		s.Clear()
		s.Options(sessions.Options{
			MaxAge:   -1,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
		err := s.Save()
		if err != nil {
			logger.Error("OIDC logout failed", "event", "oidc.logout.failed", "stage", "session_save", "error", err)
			http.Error(ctx.Writer, "Internal error", http.StatusInternalServerError)
		}
	}
}
func (handler *AuthHandler) LogoutWithRedirect(redirect_to string) gin.HandlerFunc{
	return func (ctx *gin.Context) {
		handler.Logout()(ctx)
		ctx.Redirect(http.StatusFound, redirect_to)
	}
}

func (handler *AuthHandler) IsLoggedIn(ctx *gin.Context) bool {
	session := sessions.Default(ctx)
	if session.Get(handler.session_label_userid) == nil {
		return false
	}
	expiresAt, ok := session.Get(handler.session_label_expired).(int64)
	return ok && expiresAt > time.Now().Unix()
}

func (handler *AuthHandler) GetUserID(ctx *gin.Context) (string, error) {
	if ! handler.IsLoggedIn(ctx) {
		return "", errors.New("Not logged in")
	}
	session := sessions.Default(ctx)
	uid := session.Get(handler.session_label_userid)
	if uid != nil {
		return uid.(string), nil
	} else {
		return "", errors.New("No UID")
	}
}

func (handler *AuthHandler) Ensure_loggedin() gin.HandlerFunc{
	return func(ctx *gin.Context) {
		if handler.IsLoggedIn(ctx) {
			// fmt.Println("ENSURE_LOGGEDIN: Authorized!")
			return
		}

		if ctx.GetHeader("HX-Request") == "true" {
			ctx.Header("HX-Redirect", "/login")
			ctx.Status(http.StatusOK)
			ctx.Abort()
			return
		}

		// Do not redirect an expired POST back to its GET-only URL after login.
		returnTo := handler.default_authenticated_url
		if ctx.Request.Method == http.MethodGet || ctx.Request.Method == http.MethodHead {
			returnTo = ctx.Request.URL.RequestURI()
		}
		handler.startLogin(ctx, returnTo)
	}

}

func (handler *AuthHandler) Callback_handler() func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		logger := handler.requestLogger(ctx)
		httpRequest := ctx.Request
		w := ctx.Writer

		state := httpRequest.URL.Query().Get("state")
		transaction, ok := handler.pending_auth_transactions.take(state)
		if !ok {
			logger.Warn("OIDC callback rejected", "event", "oidc.callback.rejected", "reason", "missing_or_expired_transaction")
			http.Error(w, "sign-in request is missing or expired; return to the tray and try again", http.StatusBadRequest)
			return
		}
		session := sessions.Default(ctx)
		loginBinding, ok := session.Get(handler.session_label_login_binding).(string)
		if !ok || loginBinding != transaction.sessionBinding {
			logger.Warn("OIDC callback rejected", "event", "oidc.callback.rejected", "reason", "session_binding_mismatch")
			http.Error(w, "sign-in request does not belong to this browser; return to the tray and try again", http.StatusBadRequest)
			return
		}
		if providerError := httpRequest.URL.Query().Get("error"); providerError != "" {
			logger.Warn("OIDC callback rejected", "event", "oidc.callback.rejected", "reason", "provider_error")
			http.Error(w, "sign-in was not completed; return to the tray and try again", http.StatusBadRequest)
			return
		}

		oauth2Token, err := handler.oauth2Conf.Exchange(*handler.context, httpRequest.URL.Query().Get("code"))
		if err != nil {
			logger.Error("OIDC callback failed", "event", "oidc.callback.failed", "stage", "token_exchange", "error", err)
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			logger.Error("OIDC callback failed", "event", "oidc.callback.failed", "stage", "id_token_missing")
			http.Error(w, "No id_token field in oauth2 token.", http.StatusInternalServerError)
			return
		}
		idToken, err := handler.verifier.Verify(*handler.context, rawIDToken)
		if err != nil {
			logger.Warn("OIDC callback rejected", "event", "oidc.callback.rejected", "reason", "token_verification_failed", "error", err)
			http.Error(w, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if idToken.Nonce != transaction.nonce {
			logger.Warn("OIDC callback rejected", "event", "oidc.callback.rejected", "reason", "nonce_mismatch")
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		redirection_url := transaction.redirectTo

		resp := struct {
			OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
		}{oauth2Token, new(json.RawMessage)}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			logger.Error("OIDC callback failed", "event", "oidc.callback.failed", "stage", "claims", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		session.Set(handler.session_label_userid, idToken.Subject)
		session.Set(handler.session_label_expired, time.Now().Unix() + handler.expirationTimer)
		err = session.Save()
		if err != nil {
			logger.Error("OIDC callback failed", "event", "oidc.callback.failed", "stage", "session_save", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		ctx.Redirect(http.StatusFound, redirection_url)
	}
}

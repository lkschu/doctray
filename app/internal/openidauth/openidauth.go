package openidauth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	// "time"

	"encoding/base64"
	"encoding/json"

	"github.com/gin-contrib/sessions"

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


type cookie_auth_content struct {
	State string		`json:"state"`
	Nonce string		`json:"nonce"`
	Redirect_to string	`json:"redir"`
}
func AuthFromJSON(cookie string) (cookie_auth_content, error) {
	var c cookie_auth_content
	err := json.Unmarshal([]byte(cookie),&c)
	if err != nil {
		fmt.Println("Error, can't unmarshall: ", err, "\n\n", cookie)
		return c, err
	}
	return c, nil
}
func (c cookie_auth_content) ToJSON() string {
	new_cookie_json, err := json.Marshal(c)
	if err != nil {
		panic("can't marshall cookie")
	}
	return string(new_cookie_json)
}



type AuthHandler struct {
	context		*context.Context
	provider	*oidc.Provider
	verifier	*oidc.IDTokenVerifier
	oauth2Conf  *oauth2.Config
	expirationTimer	int64
	session_label_nonce string
	session_label_expired string
	session_label_state string
	session_label_redirect string
	session_label_userid string
	session_label_sessionid string
	default_authenticated_url string
}
func (a AuthHandler) UserIDLabel() string {
	return a.session_label_userid
}
func (a AuthHandler) SessionIDLabel() string {
	return a.session_label_sessionid
}
func NewAuthHandler(clientID string, clientSecret string, sessionExpiration int64, issuerUrl string, redirectURL string) AuthHandler{
	context := context.Background()

	provider, err := oidc.NewProvider(context, issuerUrl)
	if err != nil {
		log.Fatal(err)
		panic(err)
	}
	if sessionExpiration <= 0 {
		log.Fatal("Auth expiration timer must be  >0!")
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
		session_label_nonce: "auth_nonce", session_label_expired: "expiration",session_label_state: "auth_state",
		session_label_redirect: "auth_redir", session_label_userid: "sub",
		default_authenticated_url: "/", session_label_sessionid: "sessionid" }
}

func (handler *AuthHandler) Login() gin.HandlerFunc {
	return func (ctx *gin.Context) {
		previousURL := ctx.Request.RequestURI // Current URL
		if previousURL == "" {
			previousURL = handler.default_authenticated_url
		}

		w := ctx.Writer
		lstate, err := randString(16)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		nonce, err := randString(16)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		sessionid, err := randString(16)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		s := sessions.Default(ctx)
		s.Set(handler.session_label_nonce, nonce)
		s.Set(handler.session_label_state, lstate)
		s.Set(handler.session_label_redirect, previousURL)
		s.Set(handler.session_label_sessionid, sessionid)
		err = s.Save()
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}

		ctx.Redirect(http.StatusFound, handler.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)))
	}
}

func (handler *AuthHandler) Logout() gin.HandlerFunc{
	return func (ctx *gin.Context) {
		s := sessions.Default(ctx)
		s.Clear()
		s.Options(sessions.Options{MaxAge: -1, Path: "/"}) // this sets the cookie as expired
		err := s.Save()
		if err != nil {
			log.Fatal("Can't save(remove) cookie!")
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
	uid := session.Get(handler.session_label_userid)
	if uid != nil {
		exp := session.Get(handler.session_label_expired)
		now := time.Now().Unix()
		if exp != nil {
			if exp.(int64) > now {
				return true
			} else {
				// fmt.Println("AuthHandler: expired")
			}
		} else {
			fmt.Println("AuthHandler: no expiration date")
		}
	} else {
		fmt.Println("AuthHandler: no userid")
	}
	return false
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
		} else {
			// fmt.Println("ENSURE_LOGGEDIN: Unauthorized")
			handler.Login()(ctx)
		}
	}

}

func (handler *AuthHandler) Callback_handler() func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		httpRequest := ctx.Request
		w := ctx.Writer

		session := sessions.Default(ctx)
		session_state := session.Get(handler.session_label_state)
		if session_state == nil {
			http.Error(w, "no state found", http.StatusBadRequest)
			return
		}
		if httpRequest.URL.Query().Get("state") != session_state.(string) {
			http.Error(w, "state did not match", http.StatusBadRequest)
			return
		}

		oauth2Token, err := handler.oauth2Conf.Exchange(*handler.context, httpRequest.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "No id_token field in oauth2 token.", http.StatusInternalServerError)
			return
		}
		idToken, err := handler.verifier.Verify(*handler.context, rawIDToken)
		if err != nil {
			http.Error(w, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		session_nonce := session.Get(handler.session_label_nonce)
		if session_nonce == nil {
			http.Error(w, "no state found", http.StatusBadRequest)
			return
		}
		if idToken.Nonce != session_nonce.(string) {
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		redirection_url := handler.default_authenticated_url
		if redir := session.Get(handler.session_label_redirect); redir != nil {
			redirection_url = redir.(string)
		}
		fmt.Println("Redirect to: ", redirection_url)

		resp := struct {
			OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
		}{oauth2Token, new(json.RawMessage)}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		session.Set(handler.session_label_userid, idToken.Subject)
		session.Set(handler.session_label_expired, time.Now().Unix() + handler.expirationTimer)
		err = session.Save()
		if err != nil {
		    panic(err)
		}
		ctx.Redirect(http.StatusFound, redirection_url)
	}
}

// Package auth provides the authentication logic for the application
package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"golang.org/x/oauth2"
)

const (
	loginWaitTime = 60 * time.Second
)

var (
	state = "random-state-string"
)

// StartOAuthFlow starts the OAuth flow
func StartOAuthFlow(
	ctx context.Context, oauthConf oauth2.Config, redirectPort, redirectPath string) (*AuthToken, error) {
	var authToken *AuthToken
	var ok bool

	authURL := oauthConf.AuthCodeURL(state, oauth2.AccessTypeOffline)

	fmt.Println("Opening browser for OAuth authentication...")
	if err := openBrowser(authURL); err != nil {
		fmt.Println("Failed to open browser. Please open the following URL in your browser to authenticate:")

		fmt.Printf("Authentication URL:\n+-----------------------------------------------------------+\n\n")
		fmt.Println(authURL)
		fmt.Printf("\n+-----------------------------------------------------------+\n\n")
	}

	forceTimeout := make(chan bool, 1)

	server := &http.Server{
		Addr: fmt.Sprintf(":%s", redirectPort),
	}
	http.HandleFunc(redirectPath, handlerCallbackFactory(ctx, oauthConf, func(token *oauth2.Token) {
		fmt.Println("Received token from OAuth server.")
		authToken = NewAuthToken(token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
		ok = true
	}, forceTimeout))

	go func() {
		fmt.Println("Waiting for authentication Callback...", fmt.Sprintf(":%s%s", redirectPort, redirectPath))
		fmt.Println()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	timer := time.NewTimer(loginWaitTime)

	select {
	case <-forceTimeout:
		fmt.Println("Authentication successful! Shutting down server...")
	case <-timer.C:
		fmt.Println("Timeout reached. Shutting down server...")
	}

	if err := server.Shutdown(ctx); err != nil {
		return nil, fmt.Errorf("failed to shutdown server: %v", err)
	}
	if !ok {
		return nil, fmt.Errorf("authentication failed")
	}

	return authToken, nil
}

func handlerCallbackFactory(
	ctx context.Context,
	oauthConf oauth2.Config,
	setter func(token *oauth2.Token),
	shutdownFlag chan<- bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		token, err := oauthConf.Exchange(ctx, code)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to exchange token: %v", err), http.StatusInternalServerError)
			return
		}

		setter(token)
		fmt.Fprintf(w, "Authentication successful! You can close this window.")

		shutdownFlag <- true
	}
}

func openBrowser(url string) error {
	var err error
	switch os := os.Getenv("OS"); os {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default: // linux and others
		err = exec.Command("open", url).Start()
	}

	return fmt.Errorf("failed to open browser: %v", err)
}
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ProtonMail/go-proton-api"
)

// authenticate tries cached session first, falls back to interactive login.
func authenticate(ctx context.Context, m *proton.Manager) (*proton.Client, error) {
	c, err := loginFromSession(ctx, m)
	if err != nil {
		log.Printf("No cached session: %v", err)
		return interactiveLogin(ctx, m)
	}
	return c, nil
}

func interactiveLogin(ctx context.Context, m *proton.Manager) (*proton.Client, error) {
	reader := bufio.NewReader(os.Stdin)

	username := os.Getenv("PROTON_USERNAME")
	if username == "" {
		fmt.Fprint(os.Stderr, "Username: ")
		username, _ = reader.ReadString('\n')
		username = strings.TrimSpace(username)
	}

	password := os.Getenv("PROTON_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		password, _ = reader.ReadString('\n')
		password = strings.TrimSpace(password)
	}

	c, auth, err := m.NewClientWithLogin(ctx, username, []byte(password))
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	var saltedKeyPass []byte
	latestAuth := auth

	c.AddAuthHandler(func(a proton.Auth) {
		latestAuth = a
		if err := saveSession(SessionData{
			UID:           a.UID,
			AccessToken:   a.AccessToken,
			RefreshToken:  a.RefreshToken,
			SaltedKeyPass: saltedKeyPass,
		}); err != nil {
			log.Printf("Warning: could not update session: %v", err)
		}
	})

	if auth.TwoFA.Enabled&proton.HasTOTP != 0 {
		fmt.Fprint(os.Stderr, "TOTP code: ")
		code, _ := reader.ReadString('\n')
		code = strings.TrimSpace(code)
		if err := c.Auth2FA(ctx, proton.Auth2FAReq{TwoFactorCode: code}); err != nil {
			c.Close()
			return nil, fmt.Errorf("2FA: %w", err)
		}
	}

	user, err := c.GetUser(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("get user for salt: %w", err)
	}

	salts, err := c.GetSalts(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("get salts: %w", err)
	}

	saltedKeyPass, err = salts.SaltForKey([]byte(password), user.Keys.Primary().ID)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("salt key pass: %w", err)
	}

	if err := saveSession(SessionData{
		UID:           latestAuth.UID,
		AccessToken:   latestAuth.AccessToken,
		RefreshToken:  latestAuth.RefreshToken,
		SaltedKeyPass: saltedKeyPass,
	}); err != nil {
		log.Printf("Warning: could not save session: %v", err)
	}

	return c, nil
}

func loginFromSession(ctx context.Context, m *proton.Manager) (*proton.Client, error) {
	sess, err := loadSession()
	if err != nil {
		return nil, err
	}

	c, auth, err := m.NewClientWithRefresh(ctx, sess.UID, sess.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	sess.UID = auth.UID
	sess.AccessToken = auth.AccessToken
	sess.RefreshToken = auth.RefreshToken
	if err := saveSession(sess); err != nil {
		log.Printf("Warning: could not update session: %v", err)
	}

	c.AddAuthHandler(func(a proton.Auth) {
		if err := saveSession(SessionData{
			UID:           a.UID,
			AccessToken:   a.AccessToken,
			RefreshToken:  a.RefreshToken,
			SaltedKeyPass: sess.SaltedKeyPass,
		}); err != nil {
			log.Printf("Warning: could not update session: %v", err)
		}
	})

	return c, nil
}

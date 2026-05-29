// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cacggghp/vk-turn-proxy/internal/providerstate"
	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

type VKCredentials struct {
	ClientID     string
	ClientSecret string
}

var defaultVKCredentials = []VKCredentials{}

func vkCredentialsFromEnv() []VKCredentials {
	configured := strings.TrimSpace(os.Getenv("VKTURN_VK_CREDENTIALS"))
	if configured == "" {
		return append([]VKCredentials(nil), defaultVKCredentials...)
	}

	parts := strings.Split(configured, ",")
	credentials := make([]VKCredentials, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clientID, clientSecret, ok := strings.Cut(part, ":")
		if !ok {
			log.Printf("[VK Auth] skipping malformed VKTURN_VK_CREDENTIALS entry")
			continue
		}
		clientID = strings.TrimSpace(clientID)
		clientSecret = strings.TrimSpace(clientSecret)
		if clientID == "" || clientSecret == "" {
			log.Printf("[VK Auth] skipping incomplete VKTURN_VK_CREDENTIALS entry")
			continue
		}
		credentials = append(credentials, VKCredentials{ClientID: clientID, ClientSecret: clientSecret})
	}
	return credentials
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	diagnosis := providerstate.ClassifyError(statusmodel.ProviderVK, err)
	return diagnosis.Code == statusmodel.CodeProviderAuth
}

func parseVKTurnServer(resp map[string]interface{}) (string, string, string, error) {
	tsRaw, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", "", fmt.Errorf("missing turn_server in response: %v", resp)
	}
	user, ok := tsRaw["username"].(string)
	if !ok {
		return "", "", "", fmt.Errorf("missing username in turn_server")
	}
	pass, ok := tsRaw["credential"].(string)
	if !ok {
		return "", "", "", fmt.Errorf("missing credential in turn_server")
	}
	urlsRaw, ok := tsRaw["urls"].([]interface{})
	if !ok || len(urlsRaw) == 0 {
		return "", "", "", fmt.Errorf("missing or empty urls in turn_server")
	}
	urlStr, ok := urlsRaw[0].(string)
	if !ok {
		return "", "", "", fmt.Errorf("turn server url is not a string")
	}

	clean := strings.Split(urlStr, "?")[0]
	address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")

	return user, pass, address, nil
}

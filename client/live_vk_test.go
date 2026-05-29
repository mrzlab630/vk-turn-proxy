package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
)

func TestLiveVKCredentialFlowOptIn(t *testing.T) {
	if os.Getenv("VKTURN_LIVE_VK_PROOF") != "1" {
		t.Skip("set VKTURN_LIVE_VK_PROOF=1 to run live VK proof")
	}

	link := os.Getenv("VKTURN_LIVE_VK_LINK")
	if link == "" {
		t.Fatal("VKTURN_LIVE_VK_LINK is required")
	}
	if idx := strings.Index(link, "join/"); idx != -1 {
		link = link[idx+len("join/"):]
	}
	if idx := strings.IndexAny(link, "/?#"); idx != -1 {
		link = link[:idx]
	}
	if link == "" {
		t.Fatal("VKTURN_LIVE_VK_LINK does not contain a join id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dialer := dnsdialer.New(
		dnsdialer.WithResolvers("77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53", "1.0.0.1:53"),
		dnsdialer.WithStrategy(dnsdialer.Fallback{}),
		dnsdialer.WithCache(100, 10*time.Hour, 10*time.Hour),
	)

	user, pass, addr, err := fetchVkCredsSerialized(ctx, link, 0, dialer)
	if err != nil {
		t.Fatalf("fetch VK TURN credentials: %v", err)
	}
	if user == "" || pass == "" || addr == "" {
		t.Fatalf("empty live VK credential fields: user=%q pass_empty=%v addr=%q", user, pass == "", addr)
	}
}

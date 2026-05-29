// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/pion/transport/v4"
)

type getCredsFunc func(ctx context.Context, link string, streamID int) (string, string, string, error)

type directNet struct{}

type directDialer struct {
	*net.Dialer
}

type directListenConfig struct {
	*net.ListenConfig
}

// Global state trackers
var (
	activeLocalPeer      atomic.Value
	globalCaptchaLockout atomic.Int64
	connectedStreams     atomic.Int32
	globalAppCancel      context.CancelFunc
	handshakeSem         = make(chan struct{}, 3)
	isDebug              bool
	manualCaptcha        bool
	autoCaptchaSliderPOC bool
)

type captchaSolveMode int

const (
	captchaSolveModeAuto captchaSolveMode = iota
	captchaSolveModeSliderPOC
	captchaSolveModeManual
)

func captchaSolveModeForAttempt(attempt int, manualOnly bool, enableSliderPOC bool) (captchaSolveMode, bool) {
	if manualOnly {
		return captchaSolveModeManual, attempt == 0
	}

	switch attempt {
	case 0:
		return captchaSolveModeAuto, true
	case 1:
		if enableSliderPOC {
			return captchaSolveModeSliderPOC, true
		}
		return captchaSolveModeManual, true
	case 2:
		if enableSliderPOC {
			return captchaSolveModeManual, true
		}
	}

	return 0, false
}

func captchaSolveModeLabel(mode captchaSolveMode) string {
	switch mode {
	case captchaSolveModeAuto:
		return "auto captcha"
	case captchaSolveModeSliderPOC:
		return "auto captcha slider POC"
	case captchaSolveModeManual:
		return "manual captcha"
	default:
		return "captcha"
	}
}

func newDirectNet() transport.Net {
	return directNet{}
}

func (directNet) ListenPacket(network string, address string) (net.PacketConn, error) {
	return net.ListenPacket(network, address)
}

func (directNet) ListenUDP(network string, locAddr *net.UDPAddr) (transport.UDPConn, error) {
	return net.ListenUDP(network, locAddr)
}

func (directNet) ListenTCP(network string, laddr *net.TCPAddr) (transport.TCPListener, error) {
	listener, err := net.ListenTCP(network, laddr)
	if err != nil {
		return nil, err
	}

	return directTCPListener{listener}, nil
}

func (directNet) Dial(network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

func (directNet) DialUDP(network string, laddr, raddr *net.UDPAddr) (transport.UDPConn, error) {
	return net.DialUDP(network, laddr, raddr)
}

func (directNet) DialTCP(network string, laddr, raddr *net.TCPAddr) (transport.TCPConn, error) {
	return net.DialTCP(network, laddr, raddr)
}

func (directNet) ResolveIPAddr(network, address string) (*net.IPAddr, error) {
	return net.ResolveIPAddr(network, address)
}

func (directNet) ResolveUDPAddr(network, address string) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr(network, address)
}

func (directNet) ResolveTCPAddr(network, address string) (*net.TCPAddr, error) {
	return net.ResolveTCPAddr(network, address)
}

func (directNet) Interfaces() ([]*transport.Interface, error) {
	return nil, transport.ErrNotSupported
}

func (directNet) InterfaceByIndex(index int) (*transport.Interface, error) {
	return nil, fmt.Errorf("%w: index=%d", transport.ErrInterfaceNotFound, index)
}

func (directNet) InterfaceByName(name string) (*transport.Interface, error) {
	return nil, fmt.Errorf("%w: %s", transport.ErrInterfaceNotFound, name)
}

func (directNet) CreateDialer(dialer *net.Dialer) transport.Dialer {
	return directDialer{Dialer: dialer}
}

func (directNet) CreateListenConfig(listenerConfig *net.ListenConfig) transport.ListenConfig {
	return directListenConfig{ListenConfig: listenerConfig}
}

func (d directDialer) Dial(network, address string) (net.Conn, error) {
	return d.Dialer.Dial(network, address)
}

func (d directListenConfig) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	return d.ListenConfig.Listen(ctx, network, address)
}

func (d directListenConfig) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return d.ListenConfig.ListenPacket(ctx, network, address)
}

type directTCPListener struct {
	*net.TCPListener
}

func (l directTCPListener) AcceptTCP() (transport.TCPConn, error) {
	return l.TCPListener.AcceptTCP()
}

// region Helper: HTTP Headers Injection

// applyBrowserProfile applies consistent User-Agent and Client Hints to bypass WAFs
func applyBrowserProfile(req *http.Request, profile Profile) {
	req.Header.Set("User-Agent", profile.UserAgent)
	req.Header.Set("sec-ch-ua", profile.SecChUa)
	req.Header.Set("sec-ch-ua-mobile", profile.SecChUaMobile)
	req.Header.Set("sec-ch-ua-platform", profile.SecChUaPlatform)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
}

func applyBrowserProfileFhttp(req *fhttp.Request, profile Profile) {
	req.Header.Set("User-Agent", profile.UserAgent)
	req.Header.Set("sec-ch-ua", profile.SecChUa)
	req.Header.Set("sec-ch-ua-mobile", profile.SecChUaMobile)
	req.Header.Set("sec-ch-ua-platform", profile.SecChUaPlatform)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
}

func generateBrowserFp(profile Profile) string {
	data := profile.UserAgent + profile.SecChUa + "1920x1080x24" + strconv.FormatInt(time.Now().UnixNano(), 10)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func generateFakeCursor() string {
	startX := 600 + secureIntn(400)
	startY := 300 + secureIntn(200)
	startTime := time.Now().UnixMilli() - int64(secureIntn(2000)+1000)
	var points []string
	for i := 0; i < 15+secureIntn(10); i++ {
		startX += secureIntn(15) - 5
		startY += secureIntn(15) + 2
		startTime += int64(secureIntn(40) + 10)
		points = append(points, fmt.Sprintf(`{"x":%d,"y":%d,"t":%d}`, startX, startY, startTime))
	}
	return "[" + strings.Join(points, ",") + "]"
}

func getCustomNetDialer() net.Dialer {
	return net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				var d net.Dialer
				dnsServers := []string{"77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53", "1.0.0.1:53"}
				var lastErr error
				for _, dns := range dnsServers {
					conn, err := d.DialContext(ctx, "udp", dns)
					if err == nil {
						return conn, nil
					}
					lastErr = err
				}
				return nil, lastErr
			},
		},
	}
}

// endregion

// region Automatic Captcha Solver & Authentication

type VkCaptchaError struct {
	ErrorCode               int
	ErrorMsg                string
	CaptchaSid              string
	CaptchaImg              string
	RedirectURI             string
	IsSoundCaptchaAvailable bool
	SessionToken            string
	CaptchaTs               string
	CaptchaAttempt          string
}

func ParseVkCaptchaError(errData map[string]interface{}) *VkCaptchaError {
	// Extract error_code
	codeFloat, ok := errData["error_code"].(float64)
	if !ok {
		log.Printf("missing error_code in captcha error data")
		return nil
	}
	code := int(codeFloat)

	redirectURI, _ := errData["redirect_uri"].(string)

	// Extract captcha_sid
	captchaSid, ok := errData["captcha_sid"].(string)
	if !ok {
		// try numeric
		if sidNum, ok2 := errData["captcha_sid"].(float64); ok2 {
			captchaSid = fmt.Sprintf("%.0f", sidNum)
		} else {
			log.Printf("missing captcha_sid in captcha error data")
			return nil
		}
	}

	// redirect_uri based captcha does not always include captcha_img.
	captchaImg, _ := errData["captcha_img"].(string)

	// Extract error_msg
	errorMsg, ok := errData["error_msg"].(string)
	if !ok {
		log.Printf("missing error_msg in captcha error data")
		return nil
	}

	// Extract session token if redirect_uri present
	var sessionToken string
	if redirectURI != "" {
		if parsed, err := neturl.Parse(redirectURI); err == nil {
			sessionToken = parsed.Query().Get("session_token")
		} else {
			log.Printf("failed to parse redirect_uri: %v", err)
			return nil
		}
	}

	// Extract is_sound_captcha_available
	isSound, ok := errData["is_sound_captcha_available"].(bool)
	if !ok {
		isSound = false
	}

	// Extract captcha_ts
	var captchaTs string
	if tsFloat, ok := errData["captcha_ts"].(float64); ok {
		captchaTs = fmt.Sprintf("%.0f", tsFloat)
	} else if tsStr, ok := errData["captcha_ts"].(string); ok {
		captchaTs = tsStr
	}

	// Extract captcha_attempt
	var captchaAttempt string
	if attFloat, ok := errData["captcha_attempt"].(float64); ok {
		captchaAttempt = fmt.Sprintf("%.0f", attFloat)
	} else if attStr, ok := errData["captcha_attempt"].(string); ok {
		captchaAttempt = attStr
	}

	// Build VkCaptchaError
	return &VkCaptchaError{
		ErrorCode:               code,
		ErrorMsg:                errorMsg,
		CaptchaSid:              captchaSid,
		CaptchaImg:              captchaImg,
		RedirectURI:             redirectURI,
		IsSoundCaptchaAvailable: isSound,
		SessionToken:            sessionToken,
		CaptchaTs:               captchaTs,
		CaptchaAttempt:          captchaAttempt,
	}
}

func (e *VkCaptchaError) IsCaptchaError() bool {
	return e.ErrorCode == 14 && ((e.RedirectURI != "" && e.SessionToken != "") || e.CaptchaImg != "")
}

func solveVkCaptcha(ctx context.Context, captchaErr *VkCaptchaError, streamID int, client tlsclient.HttpClient, profile Profile, useSliderPOC bool) (string, error) {
	if useSliderPOC {
		log.Printf("[STREAM %d] [Captcha] Solving captcha with slider POC...", streamID)
	} else {
		log.Printf("[STREAM %d] [Captcha] Solving captcha...", streamID)
	}

	if captchaErr.SessionToken == "" {
		return "", fmt.Errorf("no session_token in redirect_uri for auto-solve")
	}
	if captchaErr.RedirectURI == "" {
		return "", fmt.Errorf("no redirect_uri for auto-solve")
	}

	bootstrap, err := fetchCaptchaBootstrap(ctx, captchaErr.RedirectURI, client, profile)
	if err != nil {
		return "", fmt.Errorf("failed to fetch captcha bootstrap: %w", err)
	}

	log.Printf("[STREAM %d] [Captcha] PoW input: %s, difficulty: %d", streamID, bootstrap.PowInput, bootstrap.Difficulty)

	hash := solvePoW(bootstrap.PowInput, bootstrap.Difficulty)
	log.Printf("[STREAM %d] [Captcha] PoW solved: hash=%s", streamID, hash)

	var successToken string
	if useSliderPOC {
		successToken, err = callCaptchaNotRobotWithSliderPOC(
			ctx,
			captchaErr.SessionToken,
			hash,
			streamID,
			client,
			profile,
			bootstrap.Settings,
		)
	} else {
		successToken, err = callCaptchaNotRobot(ctx, captchaErr.SessionToken, hash, streamID, client, profile)
	}
	if err != nil {
		return "", fmt.Errorf("captchaNotRobot API failed: %w", err)
	}

	log.Printf("[STREAM %d] [Captcha] Success! Got success_token", streamID)
	return successToken, nil
}

func fetchCaptchaBootstrap(ctx context.Context, redirectURI string, client tlsclient.HttpClient, profile Profile) (*captchaBootstrap, error) {
	parsedURL, err := neturl.Parse(redirectURI)
	if err != nil {
		return nil, err
	}
	domain := parsedURL.Hostname()

	req, err := fhttp.NewRequestWithContext(ctx, "GET", redirectURI, nil)
	if err != nil {
		return nil, err
	}

	req.Host = domain
	applyBrowserProfileFhttp(req, profile)
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseCaptchaBootstrapHTML(string(body))
}

func solvePoW(powInput string, difficulty int) string {
	target := strings.Repeat("0", difficulty)
	for nonce := 1; nonce <= 10000000; nonce++ {
		data := powInput + strconv.Itoa(nonce)
		hash := sha256.Sum256([]byte(data))
		hexHash := hex.EncodeToString(hash[:])
		if strings.HasPrefix(hexHash, target) {
			return hexHash
		}
	}
	return ""
}

func callCaptchaNotRobot(ctx context.Context, sessionToken, hash string, streamID int, client tlsclient.HttpClient, profile Profile) (string, error) {
	vkReq := func(method string, postData string) (map[string]interface{}, error) {
		reqURL := "https://api.vk.ru/method/" + method + "?v=5.131"
		parsedURL, err := neturl.Parse(reqURL)
		if err != nil {
			return nil, fmt.Errorf("parse request URL: %w", err)
		}
		domain := parsedURL.Hostname()

		req, err := fhttp.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(postData))
		if err != nil {
			return nil, err
		}

		req.Host = domain
		applyBrowserProfileFhttp(req, profile)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://id.vk.ru")
		req.Header.Set("Referer", "https://id.vk.ru/")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-GPC", "1")
		req.Header.Set("Priority", "u=1, i")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(httpResp.Body)

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	baseParams := fmt.Sprintf("session_token=%s&domain=vk.com&adFp=&access_token=", neturl.QueryEscape(sessionToken))

	log.Printf("[STREAM %d] [Captcha] Step 1/4: settings", streamID)
	if _, err := vkReq("captchaNotRobot.settings", baseParams); err != nil {
		return "", fmt.Errorf("settings failed: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	log.Printf("[STREAM %d] [Captcha] Step 2/4: componentDone", streamID)
	browserFp := generateBrowserFp(profile)
	deviceJSON := buildCaptchaDeviceJSON(profile)
	componentDoneData := baseParams + fmt.Sprintf("&browser_fp=%s&device=%s", browserFp, neturl.QueryEscape(deviceJSON))

	if _, err := vkReq("captchaNotRobot.componentDone", componentDoneData); err != nil {
		return "", fmt.Errorf("componentDone failed: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	log.Printf("[STREAM %d] [Captcha] Step 3/4: check", streamID)
	cursorJSON := generateFakeCursor()
	answer := base64.StdEncoding.EncodeToString([]byte("{}"))

	debugInfoBytes := sha256.Sum256([]byte(profile.UserAgent + strconv.FormatInt(time.Now().UnixNano(), 10)))
	debugInfo := hex.EncodeToString(debugInfoBytes[:])

	connectionRtt := "[50,50,50,50,50,50,50,50,50,50]"
	connectionDownlink := "[9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5,9.5]"

	checkData := baseParams + fmt.Sprintf(
		"&accelerometer=%s&gyroscope=%s&motion=%s&cursor=%s&taps=%s&connectionRtt=%s&connectionDownlink=%s&browser_fp=%s&hash=%s&answer=%s&debug_info=%s",
		neturl.QueryEscape("[]"), neturl.QueryEscape("[]"), neturl.QueryEscape("[]"),
		neturl.QueryEscape(cursorJSON), neturl.QueryEscape("[]"), neturl.QueryEscape(connectionRtt),
		neturl.QueryEscape(connectionDownlink),
		browserFp, hash, answer, debugInfo,
	)

	checkResp, err := vkReq("captchaNotRobot.check", checkData)
	if err != nil {
		return "", fmt.Errorf("check failed: %w", err)
	}

	respObj, ok := checkResp["response"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid check response: %v", checkResp)
	}
	status, ok := respObj["status"].(string)
	if !ok || status != "OK" {
		return "", fmt.Errorf("check status: %s", status)
	}
	successToken, ok := respObj["success_token"].(string)
	if !ok || successToken == "" {
		return "", fmt.Errorf("success_token not found")
	}

	time.Sleep(200 * time.Millisecond)

	log.Printf("[STREAM %d] [Captcha] Step 4/4: endSession", streamID)
	_, err = vkReq("captchaNotRobot.endSession", baseParams)
	if err != nil {
		log.Printf("[STREAM %d] [Captcha] Warning: endSession failed: %v", streamID, err)
	}

	return successToken, nil
}

// endregion

// endregion

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	globalAppCancel = cancel
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signalChan
		log.Printf("Terminating...\n")
		cancel()
		select {
		case <-signalChan:
		case <-time.After(5 * time.Second):
		}
		log.Fatalf("Exit...\n")
	}()

	host := flag.String("turn", "", "override TURN server ip")
	port := flag.String("port", "", "override TURN port")
	listen := flag.String("listen", "127.0.0.1:9000", "listen on ip:port")
	vklink := flag.String("vk-link", "", "VK calls invite link \"https://vk.com/call/join/...\"")
	yalink := flag.String("yandex-link", "", "Yandex telemost invite link \"https://telemost.yandex.ru/j/...\"")
	peerAddr := flag.String("peer", "", "peer server address (host:port)")
	n := flag.Int("n", 0, "connections to TURN (default 10 for VK, 1 for Yandex)")
	udp := flag.Bool("udp", false, "connect to TURN with UDP")
	vlessMode := flag.Bool("vless", false, "VLESS mode: forward TCP connections (for VLESS) instead of UDP packets")
	devDirect := flag.Bool("dev-direct", false, "DEV ONLY: bypass TURN provider and connect DTLS directly to peer")
	debugFlag := flag.Bool("debug", false, "enable debug logging")
	manualCaptchaFlag := flag.Bool("manual-captcha", false, "skip auto captcha solving, use manual mode immediately")
	flag.Parse()
	if *peerAddr == "" {
		log.Panicf("Need peer address!")
	}
	peer, err := net.ResolveUDPAddr("udp", *peerAddr)
	if err != nil {
		panic(err)
	}
	if *devDirect && !*vlessMode {
		log.Panicf("-dev-direct requires -vless")
	}
	if *devDirect && (*vklink != "" || *yalink != "") {
		log.Panicf("-dev-direct cannot be combined with provider links")
	}
	if !*devDirect && ((*vklink == "") == (*yalink == "")) {
		log.Panicf("Need either vk-link or yandex-link!")
	}

	isDebug = *debugFlag
	manualCaptcha = *manualCaptchaFlag
	autoCaptchaSliderPOC = !manualCaptcha

	var link string
	var getCreds getCredsFunc
	if *devDirect {
		link = "dev-direct"
		if *n <= 0 {
			*n = 1
		}
	} else if *vklink != "" {
		parts := strings.Split(*vklink, "join/")
		link = parts[len(parts)-1]

		dialer := dnsdialer.New(
			dnsdialer.WithResolvers("77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53", "1.0.0.1:53"),
			dnsdialer.WithStrategy(dnsdialer.Fallback{}),
			dnsdialer.WithCache(100, 10*time.Hour, 10*time.Hour),
		)

		getCreds = func(ctx context.Context, s string, streamID int) (string, string, string, error) {
			return getVkCredsCached(ctx, s, streamID, dialer)
		}
		if *n <= 0 {
			*n = 10
		}
	} else {
		parts := strings.Split(*yalink, "j/")
		link = parts[len(parts)-1]
		getCreds = func(ctx context.Context, s string, streamID int) (string, string, string, error) {
			return getYandexCreds(s)
		}
		if *n <= 0 {
			*n = 1
		}
	}
	if !*devDirect {
		if idx := strings.IndexAny(link, "/?#"); idx != -1 {
			link = link[:idx]
		}
	}

	params := &turnParams{
		host:      *host,
		port:      *port,
		link:      link,
		udp:       *udp,
		devDirect: *devDirect,
		getCreds:  getCreds,
	}

	if *vlessMode {
		runVLESSMode(ctx, params, peer, *listen, *n)
		return
	}

	listenConn, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Panicf("Failed to listen: %s", err)
	}
	context.AfterFunc(ctx, func() {
		if closeErr := listenConn.Close(); closeErr != nil {
			log.Printf("Failed to close local connection: %s", closeErr)
		}
	})

	numStreams := *n
	if numStreams <= 0 {
		numStreams = 1
	}

	// Shared Worker Pool Queue for Aggregation
	inboundChan := make(chan *UDPPacket, 2000)

	go func() {
		for {
			pktIface := packetPool.Get()
			pkt, ok := pktIface.(*UDPPacket)
			if !ok {
				log.Printf("packetPool returned unexpected type: %T", pktIface)
				continue
			}
			nRead, addr, err := listenConn.ReadFrom(pkt.Data)
			if err != nil {
				return
			}

			// Save the local WireGuard peer address
			current := activeLocalPeer.Load()
			if current == nil {
				activeLocalPeer.Store(addr)
			} else if addrStr, ok := current.(net.Addr); ok {
				if addrStr.String() != addr.String() {
					activeLocalPeer.Store(addr)
				}
			} else {
				activeLocalPeer.Store(addr)
			}

			pkt.N = nRead

			select {
			case inboundChan <- pkt:
			default:
				// Drop the packet only if the global queue is completely full
				packetPool.Put(pkt)
			}
		}
	}()

	wg1 := sync.WaitGroup{}
	t := time.Tick(200 * time.Millisecond)

	okchan := make(chan struct{})
	connchan := make(chan net.PacketConn)
	wg1.Add(1)
	go func() {
		defer wg1.Done()
		oneDtlsConnectionLoop(ctx, peer, listenConn, inboundChan, connchan, okchan, 1)
	}()
	wg1.Add(1)
	go func() {
		defer wg1.Done()
		oneTurnConnectionLoop(ctx, params, peer, connchan, t, 1)
	}()

	select {
	case <-okchan:
	case <-ctx.Done():
	}

	for i := 1; i < numStreams; i++ {
		cchan := make(chan net.PacketConn)
		wg1.Add(1)
		go func(streamID int) {
			defer wg1.Done()
			oneDtlsConnectionLoop(ctx, peer, listenConn, inboundChan, cchan, nil, streamID)
		}(i)
		wg1.Add(1)
		go func(streamID int) {
			defer wg1.Done()
			oneTurnConnectionLoop(ctx, params, peer, cchan, t, streamID)
		}(i)
	}

	wg1.Wait()
}

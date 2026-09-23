package previewbuilder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"unicode"
	"unicode/utf8"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/netip"
	"net/url"

	readability "github.com/go-shiori/go-readability"
	htmlparser "golang.org/x/net/html"
)

const (
	previewRequestTimeout       = 5 * time.Second
	maxPreviewBodyBytes         = 1000 * 2000
	maxImageConfigBytes         = 1024 * 1024
	maxFallbackImageCandidates  = 32
	fallbackImageTimeout        = 4 * time.Second
	maxFallbackImageAspectRatio = 4.0
	previewPlaceholderImage     = "/resources/preview-placeholder.svg"
)

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var previewHTTPClient = &http.Client{
	Timeout: previewRequestTimeout,
	Transport: &http.Transport{
		DialContext: dialPublicContext,
	},
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return validatePublicURL(request.URL)
	},
}

type previewHTTPStatusError struct {
	status string
	url    *url.URL
}

func (err *previewHTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %s", err.status)
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validatePublicURL(parsedURL *url.URL) error {
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("URL must use HTTP or HTTPS")
	}
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return errors.New("URL has no host")
	}
	if ip := net.ParseIP(hostname); ip != nil && !isPublicIP(ip) {
		return errors.New("URL host is not public")
	}
	return nil
}

func dialPublicContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	var lastErr error
	for _, resolvedAddress := range resolved {
		if !isPublicIP(resolvedAddress.IP) {
			continue
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolvedAddress.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("URL host %q has no public address", host)
}

func fetchPublicURL(ctx context.Context, method, rawURL string) (*http.Response, *url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if err := validatePublicURL(parsedURL); err != nil {
		return nil, nil, err
	}

	request, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := previewHTTPClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	if response.Request == nil || response.Request.URL == nil {
		response.Body.Close()
		return nil, nil, errors.New("response has no final URL")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		finalURL := response.Request.URL
		response.Body.Close()
		return nil, nil, &previewHTTPStatusError{status: response.Status, url: finalURL}
	}

	return response, response.Request.URL, nil
}

func resolvePreviewURL(baseURL *url.URL, rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	resolvedURL := baseURL.ResolveReference(parsedURL)
	if err := validatePublicURL(resolvedURL); err != nil {
		return ""
	}
	return resolvedURL.String()
}

func fallbackImageURLs(rawHTML string, baseURL *url.URL) []string {
	document, err := htmlparser.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}

	imageURLs := make([]string, 0, maxFallbackImageCandidates)
	seenURLs := make(map[string]bool)
	var visit func(*htmlparser.Node)
	visit = func(node *htmlparser.Node) {
		if len(imageURLs) >= maxFallbackImageCandidates {
			return
		}
		if node.Type == htmlparser.ElementNode && strings.EqualFold(node.Data, "img") {
			for _, attribute := range node.Attr {
				if !strings.EqualFold(attribute.Key, "src") {
					continue
				}
				imageURL := resolvePreviewURL(baseURL, attribute.Val)
				if imageURL != "" && !seenURLs[imageURL] {
					seenURLs[imageURL] = true
					imageURLs = append(imageURLs, imageURL)
				}
				break
			}
		}
		for child := node.FirstChild; child != nil && len(imageURLs) < maxFallbackImageCandidates; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return imageURLs
}

func isSuitableFallbackImage(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	aspectRatio := float64(width) / float64(height)
	return aspectRatio <= maxFallbackImageAspectRatio && aspectRatio >= 1/maxFallbackImageAspectRatio
}

func getImageSize(ctx context.Context, imageURL string) (int, int, error) {
	response, _, err := fetchPublicURL(ctx, http.MethodGet, imageURL)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()

	config, _, err := image.DecodeConfig(io.LimitReader(response.Body, maxImageConfigBytes))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func findFallbackImage(rawHTML string, baseURL *url.URL) string {
	ctx, cancel := context.WithTimeout(context.Background(), fallbackImageTimeout)
	defer cancel()

	bestImageURL := ""
	bestPixels := int64(0)
	for _, imageURL := range fallbackImageURLs(rawHTML, baseURL) {
		width, height, err := getImageSize(ctx, imageURL)
		if err != nil || !isSuitableFallbackImage(width, height) {
			continue
		}
		if pixels := int64(width) * int64(height); pixels > bestPixels {
			bestImageURL = imageURL
			bestPixels = pixels
		}
	}
	return bestImageURL
}

func StringCleanup(s string, maxlength int) string {
	bytes := []byte(s)
	out_len := len(bytes)
	if maxlength != -1 && maxlength < out_len {
		out_len = maxlength
	}
    out := make([]rune, 0, out_len)

    for len(bytes) > 0 && len(out) < out_len {
        r, size := utf8.DecodeRune(bytes)

        switch {
        case r == utf8.RuneError && size == 1:
            // Invalid byte → skip
		case !unicode.IsPrint(r):
			// skip non-printable runes
        case unicode.IsControl(r) && r != '\n' && r != '\t':
            // Skip control chars except useful ones
        default:
            out = append(out, r)
        }

        bytes = bytes[size:]
    }

    return string(out)
}

type URLPreview struct {
	ID            string `json:"id"`
	Pending       bool   `json:"pending"`
	URL           string
	Title         string
	Description   string
	Favicon       string
	Domain        string
	Image         string
	ImageFallback string
}

type previewExtraction struct {
	Preview           URLPreview
	TitleSource       string
	DescriptionSource string
	ImageSource       string
}

func hostFallbackPreview(pageURL *url.URL) URLPreview {
	faviconURL := *pageURL
	faviconURL.User = nil
	faviconURL.Path = "/favicon.ico"
	faviconURL.RawPath = ""
	faviconURL.RawQuery = ""
	faviconURL.ForceQuery = false
	faviconURL.Fragment = ""

	favicon := faviconURL.String()
	faviconURL.Path = "/favicon"
	return URLPreview{
		URL:           pageURL.String(),
		Title:         pageURL.Hostname(),
		Favicon:       favicon,
		Domain:        pageURL.Hostname(),
		Image:         favicon,
		ImageFallback: faviconURL.String(),
	}
}

func urlFallbackPreview(rawURL string) (URLPreview, error) {
	pageURL, err := url.Parse(rawURL)
	if err != nil {
		return URLPreview{}, err
	}
	if pageURL.Scheme != "http" && pageURL.Scheme != "https" {
		return URLPreview{}, errors.New("URL must use HTTP or HTTPS")
	}
	if pageURL.Hostname() == "" {
		return URLPreview{}, errors.New("URL has no host")
	}

	return URLPreview{
		URL:    pageURL.String(),
		Title:  pageURL.Hostname(),
		Domain: pageURL.Hostname(),
		Image:  previewPlaceholderImage,
	}, nil
}

func PendingURLPreview(rawURL, id string) (URLPreview, error) {
	preview, err := urlFallbackPreview(rawURL)
	if err != nil {
		return URLPreview{}, err
	}
	preview.ID = id
	preview.Pending = true
	preview.Description = "Loading preview…"
	return preview, nil
}

func isChallengePreview(preview URLPreview, body []byte) bool {
	switch strings.ToLower(strings.TrimSpace(preview.Title)) {
	case "just a moment...", "just a moment…", "attention required!", "access denied", "pardon our interruption":
		return true
	}

	rawHTML := strings.ToLower(string(body))
	if strings.Contains(rawHTML, "challenges.cloudflare.com") || strings.Contains(rawHTML, "cf-chl-") {
		return true
	}
	return strings.EqualFold(preview.Title, "reddit") && strings.Contains(rawHTML, "js_challenge")
}

func extractPreview(body []byte, pageURL *url.URL) (previewExtraction, error) {
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err != nil {
		return previewExtraction{}, err
	}

	extraction := previewExtraction{Preview: URLPreview{
		URL:         pageURL.String(),
		Title:       StringCleanup(article.Title, 200),
		Description: StringCleanup(article.Excerpt, 500),
		Favicon:     resolvePreviewURL(pageURL, article.Favicon),
		Domain:      pageURL.Hostname(),
		Image:       resolvePreviewURL(pageURL, article.Image),
	}}
	if extraction.Preview.Title != "" {
		extraction.TitleSource = "readability"
	}
	if extraction.Preview.Description != "" {
		extraction.DescriptionSource = "readability"
	}
	if extraction.Preview.Image != "" {
		extraction.ImageSource = "readability"
	}

	enrichPreview(&extraction, string(body))
	return extraction, nil
}

func BuildURLPreview(ctx context.Context, logger *slog.Logger, inputURL, tmdbAPIKey string) (URLPreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "previewbuilder")

	if preview, handled := redditOEmbedPreview(ctx, inputURL); handled {
		logger.Info("preview resolved", "event", "preview.resolved", "host", preview.Domain, "source", "reddit_oembed")
		return preview, nil
	}
	if tmdbAPIKey != "" {
		preview, handled, err := tmdbPreviewForIMDbTitle(ctx, inputURL, tmdbAPIKey)
		if handled {
			if err == nil {
				logger.Info("preview resolved", "event", "preview.resolved", "host", preview.Domain, "source", "tmdb")
				return preview, nil
			}
			logger.Warn("preview fallback", "event", "preview.fallback", "host", previewHost(inputURL), "reason", "tmdb_error", "error", err)
			return urlFallbackPreview(inputURL)
		}
	}

	preview, err := URLPreview{}.New(ctx, logger, inputURL)
	if err == nil {
		return preview, nil
	}
	logger.Warn("preview fallback", "event", "preview.fallback", "host", previewHost(inputURL), "reason", "fetch_error", "error", err)
	return urlFallbackPreview(inputURL)
}

func previewHost(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsedURL.Hostname()
}

func (URLPreview) New(ctx context.Context, logger *slog.Logger, input_url string) (URLPreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default().With("component", "previewbuilder")
	}
	urlpreview := URLPreview{}

	// 1. Get bases
	resp, url_parsed, err := fetchPublicURL(ctx, http.MethodGet, input_url)
	if err != nil {
		var statusErr *previewHTTPStatusError
		if errors.As(err, &statusErr) {
			logger.Warn("preview fallback", "event", "preview.fallback", "host", statusErr.url.Hostname(), "reason", "http_status", "status", statusErr.status)
			return hostFallbackPreview(statusErr.url), nil
		}
		return urlpreview, errors.New("Parse failure")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPreviewBodyBytes+1))
	if err != nil {
		logger.Warn("preview fallback", "event", "preview.fallback", "host", url_parsed.Hostname(), "reason", "body_read_error", "error", err)
		return hostFallbackPreview(url_parsed), nil
	}
	if len(body) > maxPreviewBodyBytes {
		body = body[:maxPreviewBodyBytes]
	}
	extraction, err := extractPreview(body, url_parsed)
	if err != nil {
		logger.Warn("preview fallback", "event", "preview.fallback", "host", url_parsed.Hostname(), "reason", "extraction_error", "error", err)
		return hostFallbackPreview(url_parsed), nil
	}
	urlpreview = extraction.Preview
	if isChallengePreview(urlpreview, body) {
		logger.Warn("preview fallback", "event", "preview.fallback", "host", url_parsed.Hostname(), "reason", "challenge_page")
		return hostFallbackPreview(url_parsed), nil
	}
	if urlpreview.Title == "" {
		logger.Warn("preview fallback", "event", "preview.fallback", "host", url_parsed.Hostname(), "reason", "empty_title")
		return hostFallbackPreview(url_parsed), nil
	}
	if urlpreview.Image == "" {
		urlpreview.Image = findFallbackImage(string(body), url_parsed)
	}
	if urlpreview.Image == "" {
		urlpreview.Image = urlpreview.Favicon
	}

	return urlpreview, nil
}

func (up URLPreview) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("URL         : %s\n", up.URL))
	sb.WriteString(fmt.Sprintf("Domain      : %s\n", up.Domain))
	sb.WriteString(fmt.Sprintf("Title       : %s\n", up.Title))
	sb.WriteString(fmt.Sprintf("Description : %s\n", up.Description))
	sb.WriteString(fmt.Sprintf("Favicon     : %s\n", up.Favicon))
	sb.WriteString(fmt.Sprintf("Image       : %s\n", up.Image))
	return sb.String()
}

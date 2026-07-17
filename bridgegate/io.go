package bridgegate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxManifestBytes = 2 << 20
	maxJSONBytes     = 4 << 20
	maxEvidenceBytes = 32 << 20
)

type Options struct {
	Now               time.Time
	HTTPClient        *http.Client
	VerifyPublication bool
}

func strictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("JSON must contain exactly one value")
	}
	return nil
}

func fileBytes(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return nil, fmt.Errorf("must be a regular non-symlink file between 1 byte and %d bytes", maximum)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != info.Size() {
		return nil, errors.New("file changed while being read")
	}
	return raw, nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 || strings.Trim(value, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func loadEvidence(baseDirectory string, reference EvidenceRef, maximum int64) ([]byte, string, error) {
	expectedHash := normalizeSHA256(reference.SHA256)
	if strings.TrimSpace(reference.Path) == "" || expectedHash == "" {
		return nil, "", errors.New("path and non-zero sha256 are required")
	}
	raw, path, err := loadBundleFile(baseDirectory, reference.Path, maximum)
	if err != nil {
		return nil, "", err
	}
	if actual := sha256Hex(raw); actual != expectedHash {
		return nil, "", fmt.Errorf("sha256 mismatch: got %s want %s", actual, expectedHash)
	}
	return raw, path, nil
}

func loadBundleFile(baseDirectory, relativePath string, maximum int64) ([]byte, string, error) {
	path := strings.TrimSpace(relativePath)
	if path == "" {
		return nil, "", errors.New("bundle path is required")
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return nil, "", errors.New("evidence path must be relative to the manifest bundle")
	}
	baseDirectory, err := filepath.Abs(filepath.Clean(baseDirectory))
	if err != nil {
		return nil, "", err
	}
	baseDirectory, err = filepath.EvalSymlinks(baseDirectory)
	if err != nil {
		return nil, "", err
	}
	path, err = filepath.Abs(filepath.Join(baseDirectory, filepath.Clean(path)))
	if err != nil {
		return nil, "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(baseDirectory, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("evidence path must remain inside the manifest bundle")
	}
	raw, err := fileBytes(path, maximum)
	if err != nil {
		return nil, "", err
	}
	return raw, path, nil
}

func publicationURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("published evidence URL must be absolute HTTPS without credentials, query, or fragment")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || parsed.Port() != "" || hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") ||
		strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".invalid") ||
		strings.HasSuffix(hostname, ".example") || strings.HasSuffix(hostname, ".test") {
		return nil, errors.New("published evidence URL must use a public hostname on the default HTTPS port")
	}
	if ip := net.ParseIP(hostname); ip != nil && !publicIP(ip) {
		return nil, errors.New("published evidence URL must not target a private or special-use address")
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return nil, errors.New("published evidence URL must identify a report path")
	}
	return parsed, nil
}

func defaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses := []net.IP{}
		if literal := net.ParseIP(host); literal != nil {
			addresses = append(addresses, literal)
		} else {
			resolved, resolveErr := net.DefaultResolver.LookupIPAddr(ctx, host)
			if resolveErr != nil {
				return nil, resolveErr
			}
			for _, item := range resolved {
				addresses = append(addresses, item.IP)
			}
		}
		var lastErr error
		for _, ip := range addresses {
			if !publicIP(ip) || (network == "tcp4" && ip.To4() == nil) || (network == "tcp6" && ip.To4() != nil) {
				continue
			}
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("published evidence hostname did not resolve to a public address")
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("audit publication redirects are disabled")
		},
	}
}

func publicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil && ipv4[0] == 100 && ipv4[1]&0xc0 == 64 {
		return false
	}
	return true
}

func verifyPublishedHash(ctx context.Context, client *http.Client, value string, expected string) error {
	if _, err := publicationURL(value); err != nil {
		return err
	}
	if client == nil {
		client = defaultHTTPClient()
	}
	effectiveClient := *client
	effectiveClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("audit publication redirects are disabled")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/pdf,application/octet-stream,text/plain;q=0.8")
	request.Header.Set("User-Agent", "msc-bridge-release-gate/1")
	response, err := effectiveClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("published audit HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxEvidenceBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxEvidenceBytes {
		return errors.New("published audit is empty, unreadable, or too large")
	}
	if actual := sha256Hex(raw); actual != expected {
		return fmt.Errorf("published audit sha256 mismatch: got %s want %s", actual, expected)
	}
	return nil
}

func DTLSnapshotSigningBytesFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("DTL snapshot path is required")
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	raw, err := fileBytes(path, maxJSONBytes)
	if err != nil {
		return nil, err
	}
	var snapshot DTLAuthoritySnapshot
	if err := strictJSON(raw, &snapshot); err != nil {
		return nil, err
	}
	return DTLSnapshotSigningBytes(snapshot)
}

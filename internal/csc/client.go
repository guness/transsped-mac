package csc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) post(path string, req any, out any) error {
	buf, _ := json.Marshal(req)
	resp, err := c.HTTP.Post(c.BaseURL+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

type Info struct {
	CertB64   []string
	KeyAlgo   string
	KeyLen    int
	SCAL      string
	Multisign int
	AuthMode  string
}

func (c *Client) List(userID string) ([]string, error) {
	var r struct {
		CredentialIDs []string `json:"credentialIDs"`
	}
	if err := c.post("credentials/list", map[string]string{"userID": userID}, &r); err != nil {
		return nil, err
	}
	return r.CredentialIDs, nil
}

func (c *Client) SendOTP(credentialID string) error {
	return c.post("credentials/sendOTP", map[string]string{"credentialID": credentialID}, nil)
}

func (c *Client) Authorize(credentialID, pin, otp, hashB64 string) (string, error) {
	var r struct {
		SAD string `json:"SAD"`
	}
	req := map[string]any{
		"credentialID": credentialID, "numSignatures": "1",
		"hash": []string{hashB64}, "PIN": pin, "OTP": otp,
	}
	if err := c.post("credentials/authorize", req, &r); err != nil {
		return "", err
	}
	if r.SAD == "" {
		return "", fmt.Errorf("authorize returned empty SAD")
	}
	return r.SAD, nil
}

func (c *Client) SignHash(credentialID, sad, hashB64, signAlgo, hashAlgo string) (string, error) {
	var r struct {
		Signatures []string `json:"signatures"`
	}
	req := map[string]any{
		"credentialID": credentialID, "signAlgo": signAlgo, "hashAlgo": hashAlgo,
		"signAlgoParams": "", "SAD": sad, "hash": []string{hashB64},
	}
	if err := c.post("signatures/signHash", req, &r); err != nil {
		return "", err
	}
	if len(r.Signatures) == 0 || r.Signatures[0] == "" {
		return "", fmt.Errorf("signHash returned no signature")
	}
	return r.Signatures[0], nil
}

// toInt coerces a JSON value that may arrive as a number (float64) or a numeric
// string into an int; returns 0 for anything else (missing/unparseable).
func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// plausibleCert is a cheap-then-strict filter: the length check pre-filters, then
// the candidate must base64-decode to at least 100 bytes to count as a DER cert.
func plausibleCert(s string) bool {
	if len(s) <= 200 {
		return false
	}
	d, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(d) >= 100
}

func (c *Client) Info(credentialID string) (*Info, error) {
	var raw map[string]any
	req := map[string]any{"credentialID": credentialID, "certInfo": "true", "certificates": "chain"}
	if err := c.post("credentials/info", req, &raw); err != nil {
		return nil, err
	}
	info := &Info{}
	// certificates: look for []string of base64 DER under cert/certificates.
	// Every candidate must survive plausibleCert so a random long field can't
	// be mistaken for a certificate.
	collect := func(v any) {
		switch t := v.(type) {
		case string:
			if plausibleCert(t) {
				info.CertB64 = append(info.CertB64, t)
			}
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok && plausibleCert(s) {
					info.CertB64 = append(info.CertB64, s)
				}
			}
		case map[string]any:
			if cc, ok := t["certificates"]; ok {
				if arr, ok := cc.([]any); ok {
					for _, e := range arr {
						if s, ok := e.(string); ok && plausibleCert(s) {
							info.CertB64 = append(info.CertB64, s)
						}
					}
				}
			}
		}
	}
	collect(raw["cert"])
	collect(raw["certificates"])
	if s, ok := raw["SCAL"].(string); ok {
		info.SCAL = s
	}
	if s, ok := raw["authMode"].(string); ok {
		info.AuthMode = s
	} else if s, ok := raw["authmode"].(string); ok {
		info.AuthMode = s
	}
	info.Multisign = toInt(raw["multisign"])
	if k, ok := raw["key"].(map[string]any); ok {
		if a, ok := k["algo"].(string); ok {
			info.KeyAlgo = a
		}
		info.KeyLen = toInt(k["len"])
	}
	return info, nil
}

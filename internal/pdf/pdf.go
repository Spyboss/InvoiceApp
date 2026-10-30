// Package pdf renders the paper the business hands to customers. The proforma
// follows the leasing company's letterhead exactly; every other document uses
// the dealership's own layout. Rendering is delegated to Gotenberg, which runs
// headless Chromium, so the templates are ordinary html and css.
package pdf

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/spyboss/invoiceapp/internal/store"
)

// ErrNotConfigured means GOTENBERG_URL is empty. The rest of the app keeps
// working; only printing is unavailable.
var ErrNotConfigured = errors.New("pdf rendering is not configured")

//go:embed assets
var assets embed.FS

// Renderer turns one stored document into pdf bytes.
type Renderer struct {
	endpoint string
	username string
	password string
	http     *http.Client
}

// New builds a renderer. endpoint is the Gotenberg base url, for example
// http://gotenberg:3000 on Railway's private network.
func New(endpoint, username, password string) *Renderer {
	return &Renderer{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		username: username,
		password: password,
		http: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// Filename is what the browser saves the download as.
func Filename(doc store.Document) string {
	return strings.ToLower(doc.DocType) + "-" + doc.DocNo + ".pdf"
}

// Render builds the html for the document and converts it with Gotenberg.
func (r *Renderer) Render(ctx context.Context, tenantName string, doc store.Document) ([]byte, error) {
	if r == nil || r.endpoint == "" {
		return nil, ErrNotConfigured
	}

	var page string
	if doc.DocType == store.DocProforma {
		page = proformaHTML(doc)
	} else {
		page = dealerHTML(tenantName, doc)
	}
	return r.toPDF(ctx, page)
}

// toPDF posts one completely self contained html file to the chromium route.
// Nothing is fetched over the network: every image and font is a data url, so
// a stale or hostile url can never end up on paper.
func (r *Renderer) toPDF(ctx context.Context, page string) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="files"; filename="index.html"`)
	header.Set("Content-Type", "text/html; charset=utf-8")
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("build pdf request: %w", err)
	}
	if _, err := io.WriteString(part, page); err != nil {
		return nil, fmt.Errorf("build pdf request: %w", err)
	}

	fields := map[string]string{
		"paperWidth":        "8.5",
		"paperHeight":       "11",
		"marginTop":         "0",
		"marginBottom":      "0",
		"marginLeft":        "0",
		"marginRight":       "0",
		"preferCssPageSize": "true",
		"printBackground":   "true",
		"omitBackground":    "false",
		"scale":             "1",
	}
	for name, value := range fields {
		if err := mw.WriteField(name, value); err != nil {
			return nil, fmt.Errorf("build pdf request: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("build pdf request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, fmt.Errorf("pdf request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if r.username != "" {
		req.SetBasicAuth(r.username, r.password)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pdf renderer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("pdf renderer: %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	const maxPDF = 10 << 20
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, maxPDF+1))
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}
	if len(pdf) > maxPDF {
		return nil, errors.New("pdf renderer: response is larger than 10 MB")
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return nil, errors.New("pdf renderer: response is not a pdf")
	}
	return pdf, nil
}

// assetBase64 reads one embedded asset and encodes it for a data url. The
// result is computed once per process because the fonts are large.
var (
	assetMu    sync.Mutex
	assetCache = map[string]string{}
)

func assetDataURL(name, mime string) string {
	key := mime + ":" + name
	assetMu.Lock()
	defer assetMu.Unlock()
	if cached, ok := assetCache[key]; ok {
		return cached
	}
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic("pdf: missing asset " + name)
	}
	url := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	assetCache[key] = url
	return url
}

// fontCSS embeds Carlito, a metric compatible stand in for Calibri. Without
// this the linux chromium inside Gotenberg falls back to a different font and
// every line break on the proforma moves.
func fontCSS() string {
	regular := assetDataURL("Carlito-Regular.ttf", "font/ttf")
	bold := assetDataURL("Carlito-Bold.ttf", "font/ttf")
	return `@font-face{font-family:Carlito;font-style:normal;font-weight:400;src:url(` + regular + `) format("truetype")}` +
		`@font-face{font-family:Carlito;font-style:normal;font-weight:700;src:url(` + bold + `) format("truetype")}`
}

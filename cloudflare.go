// Package scraper simply implements a http.Transport interface which, when used to perform
// requests, will attempt to solve the challenge in the response by executing the JavaScript using
// github.com/robertkrimen/otto as a runtime.
package scraper

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/robertkrimen/otto"
)

const userAgent = `Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36`

var (
	jschlRegexp      = regexp.MustCompile(`name="jschl_vc" value="(\w+)"`)
	passRegexp       = regexp.MustCompile(`name="pass" value="(.+?)"`)
	jsRegexp         = regexp.MustCompile(`setTimeout\(function\(\){\s+(var s,t,o,p,b,r,e,a,k,i,n,g,f.+?\r?\n[\s\S]+?a\.value =.+?)\r?\n`)
	jsReplace1Regexp = regexp.MustCompile(`a\.value = (parseInt\(.+?\)).+`)
	jsReplace2Regexp = regexp.MustCompile(`\s{3,}[a-z](?: = |\.).+`)
	jsReplace3Regexp = regexp.MustCompile(`[\n\\']`)
)

// Transport implements the http.Transport interface
type Transport struct {
	upstream http.RoundTripper
	cookies  http.CookieJar
}

// NewTransport creates a new Transport object for use in a http.Client initialisation
func NewTransport(upstream http.RoundTripper) (*Transport, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new cookie jar")
	}
	return &Transport{upstream, jar}, nil
}

// RoundTrip implements the RoundTripper interface.
// Detects if Cloudflare's anti-bot system is active for the request and attempts to solve the
// challenge if so.
func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", userAgent)
	}

	resp, err := t.upstream.RoundTrip(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new roundtrip from given transport")
	}

	// Check if Cloudflare anti-bot is on
	if resp.StatusCode == 503 && resp.Header.Get("Server") == "cloudflare-nginx" {
		resp, err := t.solveChallenge(resp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to solve challenge")
		}

		return resp, nil
	}

	return resp, nil
}

// solveChallenge simulates a browser session by waiting a few seconds then executing the JavaScript
// sent by the Cloudflare server and responding with the answer.
func (t Transport) solveChallenge(resp *http.Response) (*http.Response, error) {
	time.Sleep(time.Second * 4) // Cloudflare requires a delay before solving the challenge

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read challenge response body")
	}

	var params = make(url.Values)

	if m := jschlRegexp.FindStringSubmatch(string(b)); len(m) > 0 {
		params.Set("jschl_vc", m[1])
	}

	if m := passRegexp.FindStringSubmatch(string(b)); len(m) > 0 {
		params.Set("pass", m[1])
	}

	chkURL, _ := url.Parse("/cdn-cgi/l/chk_jschl")
	u := resp.Request.URL.ResolveReference(chkURL)

	js, err := t.extractJS(string(b))
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract js from challenge response body")
	}

	answer, err := t.evaluateJS(js)
	if err != nil {
		return nil, errors.Wrap(err, "failed to evaluate js from challenge response body")
	}

	params.Set("jschl_answer", strconv.Itoa(int(answer)+len(resp.Request.URL.Host)))

	req, err := http.NewRequest("GET", fmt.Sprintf("%s?%s", u.String(), params.Encode()), nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new GET request for cloudflare challenge answer")
	}

	req.Header.Set("User-Agent", resp.Request.Header.Get("User-Agent"))
	req.Header.Set("Referer", resp.Request.URL.String())

	client := http.Client{
		Transport: t.upstream,
		Jar:       t.cookies,
	}

	resp, err = client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to perform cloudflare answer request")
	}

	return resp, nil
}

// extractJS attempts to extract the JavaScript from a given HTML body element based on a regular
// expression.
// todo: use an xml parser to extract this instead of just a regex, a future change could break
// this and the method used currently would make debugging it difficult.
func (t Transport) extractJS(body string) (string, error) {
	matches := jsRegexp.FindStringSubmatch(body)
	if len(matches) == 0 {
		return "", errors.New("No matching javascript found")
	}

	js := matches[1]
	js = jsReplace1Regexp.ReplaceAllString(js, "$1")
	js = jsReplace2Regexp.ReplaceAllString(js, "")

	// Strip characters that could be used to exit the string context
	// These characters are not currently used in Cloudflare's arithmetic snippet
	js = jsReplace3Regexp.ReplaceAllString(js, "")

	return js, nil
}

// evaluateJS attempts to execute the given JavaScript string and returns the result as an integer.
func (t Transport) evaluateJS(js string) (int64, error) {
	vm := otto.New()
	result, err := vm.Run(js)
	if err != nil {
		return 0, errors.Wrap(err, "failed to execute cloudflare js")
	}
	return result.ToInteger()
}

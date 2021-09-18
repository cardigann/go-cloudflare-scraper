package scraper

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/robertkrimen/otto"
)

const userAgent = `Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36`

type Transport struct {
	upstream http.RoundTripper
	Cookies  http.CookieJar
}

func NewClient() (c *http.Client, err error) {

	scraper_transport, err := NewTransport(http.DefaultTransport)
	if err != nil {
		return
	}

	c = &http.Client{
		Transport: scraper_transport,
		Jar:       scraper_transport.Cookies,
	}

	return
}

func NewTransport(upstream http.RoundTripper) (*Transport, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Transport{upstream, jar}, nil
}

func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", userAgent)
	}

	if r.Header.Get("Referer") == "" {
		r.Header.Set("Referer", r.URL.String())
	}

	resp, err := t.upstream.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	// Check if Cloudflare anti-bot is on
	server_header := resp.Header.Get("Server")
	if resp.StatusCode == 503 && (server_header == "cloudflare-nginx" || server_header == "cloudflare") {
		log.Printf("Solving challenge for %s", resp.Request.URL.Hostname())
		resp, err := t.solveChallenge(resp)

		return resp, err
	}

	return resp, err
}

var jschlRegexp = regexp.MustCompile(`name="jschl_vc" value="(\w+)"`)
var passRegexp = regexp.MustCompile(`name="pass" value="(.+?)"`)

func (t Transport) solveChallenge(resp *http.Response) (*http.Response, error) {
	time.Sleep(time.Second * 4) // Cloudflare requires a delay before solving the challenge

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = ioutil.NopCloser(bytes.NewReader(b))

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
		return nil, err
	}

	answer, err := t.evaluateJS(js)
	if err != nil {
		return nil, err
	}

	params.Set("jschl_answer", strconv.Itoa(int(answer)+len(resp.Request.URL.Host)))

	req, err := http.NewRequest("GET", fmt.Sprintf("%s?%s", u.String(), params.Encode()), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", resp.Request.Header.Get("User-Agent"))
	req.Header.Set("Referer", resp.Request.URL.String())

	log.Printf("Requesting %s?%s", u.String(), params.Encode())
	client := http.Client{
		Transport: t.upstream,
		Jar:       t.Cookies,
	}

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (t Transport) evaluateJS(js string) (int64, error) {
	vm := otto.New()
	result, err := vm.Run(js)
	if err != nil {
		return 0, err
	}
	return result.ToInteger()
}

var jsRegexp = regexp.MustCompile(
	`setTimeout\(function\(\){\s+(var ` +
		`s,t,o,p,b,r,e,a,k,i,n,g,f.+?\r?\n[\s\S]+?a\.value =.+?)\r?\n`,
)
var jsReplace1Regexp = regexp.MustCompile(`a\.value = (parseInt\(.+?\)).+`)
var jsReplace2Regexp = regexp.MustCompile(`\s{3,}[a-z](?: = |\.).+`)
var jsReplace3Regexp = regexp.MustCompile(`[\n\\']`)

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

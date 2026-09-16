package rewrite

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/metacubex/mihomo/log"
)

var (
	emptyDict   = "{}"
	emptyArray  = "[]"
	onePixelPNG = string([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48,
		0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00,
		0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x11, 0x49, 0x44, 0x41, 0x54, 0x78,
		0x9c, 0x62, 0x62, 0x60, 0x60, 0x60, 0x00, 0x04, 0x00, 0x00, 0xff, 0xff, 0x00, 0x0f,
		0x00, 0x03, 0xfe, 0x8f, 0xeb, 0xcf, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	})
)

// HandleRequest applies request-side rules to req. It returns true when the
// request was short-circuited (reject/redirect, response already written);
// false when the request was only modified or did not match any rule.
func (r *Rules) HandleRequest(rw http.ResponseWriter, req *http.Request) bool {
	url := req.URL.String()
	rule, sub, found := matchRewriteRule(r.request, url, true)
	if !found {
		return false
	}

	switch rule.typ {
	case Reject:
		http.NotFound(rw, req)
	case Reject200:
		rw.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		var payload string
		if len(rule.payload) > 0 {
			payload = rule.payload[0]
		}
		if payload != "" {
			if s := payload[:1]; s == "{" || s == "[" {
				rw.Header().Set("Content-Type", "application/json; charset=UTF-8")
			}
			rw.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(rw, payload)
		} else {
			rw.WriteHeader(http.StatusOK)
		}
	case Reject204:
		rw.WriteHeader(http.StatusNoContent)
	case RejectImg:
		rw.Header().Set("Content-Type", "image/png")
		rw.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(rw, onePixelPNG)
	case RejectDict:
		rw.Header().Set("Content-Type", "application/json; charset=UTF-8")
		rw.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(rw, emptyDict)
	case RejectArray:
		rw.Header().Set("Content-Type", "application/json; charset=UTF-8")
		rw.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(rw, emptyArray)
	case Redirect302:
		http.Redirect(rw, req, rule.replaceURLPayload(sub), http.StatusFound)
	case Redirect307:
		http.Redirect(rw, req, rule.replaceURLPayload(sub), http.StatusTemporaryRedirect)
	case RequestHeader:
		return r.handleRequestHeader(rw, req, rule, url)
	case RequestHeaderJSON:
		return r.handleRequestHeaderJSON(req, rule, url)
	case RequestBody:
		return r.handleRequestBody(req, rule, url)
	case RequestBodyJSON:
		return r.handleRequestBodyJSON(rw, req, rule, url)
	default:
		return false
	}

	log.Debugln("[MITM] rewrite request, type: %s, method: %s, url: %s", rule.typ, req.Method, url)
	return true
}

// HandleResponse applies response-side rules to resp in place.
func (r *Rules) HandleResponse(resp *http.Response) error {
	url := resp.Request.URL.String()
	rule, _, found := matchRewriteRule(r.response, url, false)
	if !found {
		return nil
	}

	switch rule.typ {
	case ResponseHeader:
		return r.handleResponseHeader(resp, rule, url)
	case ResponseHeaderJSON:
		return r.handleResponseHeaderJSON(resp, rule, url)
	case ResponseBody:
		return r.handleResponseBody(resp, rule, url)
	case ResponseBodyJSON:
		return r.handleResponseBodyJSON(resp, rule, url)
	}
	return nil
}

// matchRewriteRule finds the first rule whose URL pattern matches url.
// On the request side, sub receives the URL submatches for $N expansion in
// redirect targets.
func matchRewriteRule(rules []*Rule, url string, isRequest bool) (*Rule, []string, bool) {
	for _, rule := range rules {
		if isRequest {
			sub := findStringSubmatch(rule.urlPattern, url)
			if len(sub) != 0 {
				return rule, sub, true
			}
		} else {
			if m, _ := rule.urlPattern.MatchString(url); m {
				return rule, nil, true
			}
		}
	}
	return nil, nil, false
}

func (r *Rules) handleRequestHeader(rw http.ResponseWriter, req *http.Request, rule *Rule, url string) bool {
	if len(req.Header) == 0 || rule.match == nil {
		return false
	}

	rawHeader := &bytes.Buffer{}
	if err := req.Header.Write(rawHeader); err != nil {
		log.Errorln("[MITM] rewrite request-header, url: %s, error: %v", url, err)
		rwBadGateway(rw, req)
		return true
	}

	newRawHeader, ok := rule.replaceSubPayload(rawHeader.String())
	if !ok {
		return false
	}

	tb := textproto.NewReader(bufio.NewReader(strings.NewReader(newRawHeader)))
	newHeader, err := tb.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		log.Errorln("[MITM] rewrite request-header, url: %s, error: %v", url, err)
		rwBadGateway(rw, req)
		return true
	}

	if req.ContentLength > 0 {
		newHeader.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	} else {
		newHeader.Del("Content-Length")
	}
	req.Header = http.Header(newHeader)

	log.Debugln("[MITM] rewrite request-header, url: %s", url)
	return false
}

func (r *Rules) handleRequestHeaderJSON(req *http.Request, rule *Rule, url string) bool {
	if len(req.Header) == 0 || rule.expr == nil {
		return false
	}

	originHeader := req.Header.Clone()

	rs, err := exprRun(rule.expr, originHeader)
	if err != nil {
		log.Debugln("[MITM] rewrite json-request-header failed, url: %s, error: %v", url, err)
		return false
	}
	if !rs {
		return false
	}

	if req.ContentLength > 0 {
		originHeader.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	} else {
		originHeader.Del("Content-Length")
	}
	req.Header = originHeader

	log.Debugln("[MITM] rewrite json-request-header, url: %s", url)
	return false
}

func (r *Rules) handleRequestBody(req *http.Request, rule *Rule, url string) bool {
	if req.Method == http.MethodHead || req.Method == http.MethodOptions ||
		req.Method == http.MethodConnect || rule.match == nil {
		return false
	}

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || !canRewriteBody(mediaType) {
		return false
	}

	contentEncoding := strings.ToLower(req.Header.Get("Content-Encoding"))
	charsetEncoding := pickCharsetEncoding(params["charset"])

	originBody := &bytes.Buffer{}
	data, err := readBody(io.TeeReader(req.Body, originBody), charsetEncoding, contentEncoding)
	if err != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		req.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite request-body failed, url: %s, error: %v", url, err)
		return false
	}

	modifiedBody, ok := rule.replaceSubPayload(data.String())
	if !ok || modifiedBody == "" {
		req.Body = io.NopCloser(originBody)
		return false
	}

	newBody := &bytes.Buffer{}
	if err = writeBody(newBody, []byte(modifiedBody), charsetEncoding); err != nil {
		newBody.Reset()
		req.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite request-body failed, url: %s, error: %v", url, err)
		return false
	}

	originBody.Reset()

	req.Body = io.NopCloser(newBody)
	req.ContentLength = int64(newBody.Len())
	req.Header.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	req.Header.Del("Content-Encoding")

	log.Debugln("[MITM] rewrite request-body, url: %s", url)
	return false
}

func (r *Rules) handleRequestBodyJSON(rw http.ResponseWriter, req *http.Request, rule *Rule, url string) bool {
	if req.Method == http.MethodHead || req.Method == http.MethodOptions ||
		req.Method == http.MethodConnect || rule.expr == nil {
		return false
	}

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false
	}

	contentEncoding := strings.ToLower(req.Header.Get("Content-Encoding"))
	charsetEncoding := pickCharsetEncoding(params["charset"])

	originBody := &bytes.Buffer{}
	data, err := readJSONBody(io.TeeReader(req.Body, originBody), charsetEncoding, contentEncoding, false)
	if err != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		originBody.Reset()
		log.Errorln("[MITM] rewrite json-request-body, url: %s, error: %v", url, err)
		rwBadGateway(rw, req)
		return true
	}

	rs, err := exprRun(rule.expr, data)
	if err != nil || !rs {
		req.Body = io.NopCloser(originBody)
		if err != nil {
			log.Debugln("[MITM] rewrite json-request-body failed, url: %s, error: %v", url, err)
		}
		return false
	}

	newBody := &bytes.Buffer{}
	if err = writeJSONBody(newBody, data, charsetEncoding, false); err != nil {
		newBody.Reset()
		req.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite json-request-body failed, url: %s, error: %v", url, err)
		return false
	}

	originBody.Reset()

	req.Body = io.NopCloser(newBody)
	req.ContentLength = int64(newBody.Len())
	req.Header.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	req.Header.Del("Content-Encoding")

	log.Debugln("[MITM] rewrite json-request-body, url: %s", url)
	return false
}

func (r *Rules) handleResponseHeader(resp *http.Response, rule *Rule, url string) error {
	if len(resp.Header) == 0 || rule.match == nil {
		return nil
	}

	rawHeader := &bytes.Buffer{}
	if err := resp.Header.Write(rawHeader); err != nil {
		return err
	}

	newRawHeader, ok := rule.replaceSubPayload(rawHeader.String())
	if !ok {
		return nil
	}

	tb := textproto.NewReader(bufio.NewReader(strings.NewReader(newRawHeader)))
	newHeader, err := tb.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	if resp.ContentLength > 0 {
		newHeader.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	} else {
		newHeader.Del("Content-Length")
	}
	resp.Header = http.Header(newHeader)

	log.Debugln("[MITM] rewrite response-header, url: %s", url)
	return nil
}

func (r *Rules) handleResponseHeaderJSON(resp *http.Response, rule *Rule, url string) error {
	if len(resp.Header) == 0 || rule.expr == nil {
		return nil
	}

	originHeader := resp.Header.Clone()

	rs, err := exprRun(rule.expr, originHeader)
	if err != nil {
		return err
	}
	if !rs {
		return nil
	}

	if resp.ContentLength > 0 {
		originHeader.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	} else {
		originHeader.Del("Content-Length")
	}
	resp.Header = originHeader

	log.Debugln("[MITM] rewrite json-response-header, url: %s", url)
	return nil
}

func (r *Rules) handleResponseBody(resp *http.Response, rule *Rule, url string) error {
	req := resp.Request
	if req.Method == http.MethodHead || req.Method == http.MethodOptions ||
		req.Method == http.MethodConnect || rule.match == nil {
		return nil
	}

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !canRewriteBody(mediaType) {
		return nil
	}

	contentEncoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	charsetEncoding := pickCharsetEncoding(params["charset"])

	originBody := &bytes.Buffer{}
	data, err := readBody(io.TeeReader(resp.Body, originBody), charsetEncoding, contentEncoding)
	if err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite response-body failed, url: %s, error: %v", url, err)
		return nil
	}
	_ = resp.Body.Close()

	modifiedBody, ok := rule.replaceSubPayload(data.String())
	if !ok || modifiedBody == "" {
		resp.Body = io.NopCloser(originBody)
		return nil
	}

	newBody := &bytes.Buffer{}
	if err = writeBody(newBody, []byte(modifiedBody), charsetEncoding); err != nil {
		newBody.Reset()
		resp.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite response-body failed, url: %s, error: %v", url, err)
		return nil
	}

	originBody.Reset()

	resp.Body = io.NopCloser(newBody)
	resp.ContentLength = int64(newBody.Len())
	resp.Header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	resp.Header.Del("Content-Encoding")

	log.Debugln("[MITM] rewrite response-body, url: %s", url)
	return nil
}

func (r *Rules) handleResponseBodyJSON(resp *http.Response, rule *Rule, url string) error {
	req := resp.Request
	if req.Method == http.MethodHead || req.Method == http.MethodOptions ||
		req.Method == http.MethodConnect || rule.expr == nil {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return nil
	}

	contentEncoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	_, isBase64 := params["base64"]
	charsetEncoding := pickCharsetEncoding(params["charset"])

	originBody := &bytes.Buffer{}
	data, err := readJSONBody(io.TeeReader(resp.Body, originBody), charsetEncoding, contentEncoding, isBase64)
	if err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		originBody.Reset()
		return fmt.Errorf("rewrite response failed, error: %w", err)
	}
	_ = resp.Body.Close()

	rs, err := exprRun(rule.expr, data)
	if err != nil || !rs {
		resp.Body = io.NopCloser(originBody)
		if err != nil {
			log.Debugln("[MITM] rewrite json-response-body failed, url: %s, error: %v", url, err)
		}
		return nil
	}

	newBody := &bytes.Buffer{}
	if err = writeJSONBody(newBody, data, charsetEncoding, isBase64); err != nil {
		newBody.Reset()
		resp.Body = io.NopCloser(originBody)

		log.Debugln("[MITM] rewrite json-response-body failed, url: %s, error: %v", url, err)
		return nil
	}

	originBody.Reset()

	resp.Body = io.NopCloser(newBody)
	resp.ContentLength = int64(newBody.Len())
	resp.Header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	resp.Header.Del("Content-Encoding")

	log.Debugln("[MITM] rewrite json-response-body, url: %s", url)
	return nil
}

// rwBadGateway answers the client with 502 on unrecoverable request
// rewrite errors.
func rwBadGateway(rw http.ResponseWriter, req *http.Request) {
	log.Errorln("[MITM] handle request, url: %s", req.URL.String())
	rw.WriteHeader(http.StatusBadGateway)
}

func readBody(r io.Reader, charsetEnc encoding.Encoding, contentEncoding string) (body *bytes.Buffer, err error) {
	if contentEncoding != "" {
		cr, err := pickContentEncoding(contentEncoding, r)
		if err != nil {
			return body, err
		}
		defer cr.Close()
		r = cr
	}

	if charsetEnc != nil {
		r = transform.NewReader(r, charsetEnc.NewDecoder())
	}

	body = &bytes.Buffer{}
	_, err = body.ReadFrom(r)
	return body, err
}

func writeBody(w io.Writer, body []byte, charsetEnc encoding.Encoding) error {
	if charsetEnc != nil {
		cw := transform.NewWriter(w, charsetEnc.NewEncoder())
		defer cw.Close()
		w = cw
	}

	_, err := w.Write(body)
	return err
}

func readJSONBody(r io.Reader, charsetEnc encoding.Encoding, contentEncoding string, isBase64 bool) (body any, err error) {
	if contentEncoding != "" {
		cr, err := pickContentEncoding(contentEncoding, r)
		if err != nil {
			return nil, err
		}
		defer cr.Close()
		r = cr
	}

	if isBase64 {
		r = base64.NewDecoder(base64.StdEncoding, r)
	}

	if charsetEnc != nil {
		r = transform.NewReader(r, charsetEnc.NewDecoder())
	}

	br := bufio.NewReader(r)
	buf, err := br.Peek(2)
	if err != nil {
		return nil, err
	}
	if br.Buffered() > 2 {
		buf, err = br.Peek(br.Buffered())
		if err != nil {
			return nil, err
		}
	}
	buf = bytes.TrimLeft(buf, " \n")
	if bytes.HasPrefix(buf, []byte("{")) {
		body = map[string]any{}
	} else if bytes.HasPrefix(buf, []byte("[")) {
		body = []any{}
	} else {
		return nil, errors.New("body is not a json data")
	}

	err = json.NewDecoder(br).Decode(&body)
	return body, err
}

func writeJSONBody(w io.Writer, body any, charsetEnc encoding.Encoding, isBase64 bool) error {
	if isBase64 {
		bw := base64.NewEncoder(base64.StdEncoding, w)
		defer bw.Close()
		w = bw
	}

	if charsetEnc != nil {
		cw := transform.NewWriter(w, charsetEnc.NewEncoder())
		defer cw.Close()
		w = cw
	}

	return json.NewEncoder(w).Encode(body)
}

// zstdWrapper adapts zstd.Decoder to io.ReadCloser.
type zstdWrapper struct {
	*zstd.Decoder
}

func (z *zstdWrapper) Close() error {
	z.Decoder.Close()
	return nil
}

// pickContentEncoding wraps r with the decoder for contentEncoding; the
// caller must Close the result when done.
func pickContentEncoding(contentEncoding string, r io.Reader) (io.ReadCloser, error) {
	switch contentEncoding {
	case "gzip":
		return gzip.NewReader(r)
	case "zstd":
		z, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return &zstdWrapper{z}, nil
	case "br":
		return io.NopCloser(brotli.NewReader(r)), nil
	case "deflate":
		return flate.NewReader(r), nil
	}
	return nil, fmt.Errorf("content encoding not supported: %s", contentEncoding)
}

func pickCharsetEncoding(charset string) encoding.Encoding {
	if charset == "" || charset == "utf-8" {
		return nil
	}
	if enc, err := htmlindex.Get(charset); err == nil {
		return enc
	}
	return nil
}

// exprRun evaluates the expr program against a JSON object, or against each
// element of a JSON array (true when any element matches).
func exprRun(program *vm.Program, data any) (bool, error) {
	switch v := data.(type) {
	case map[string]any:
		output, err := expr.Run(program, Env{Data: v})
		if err != nil {
			return false, err
		}
		return output.(bool), nil
	case []any:
		var rs bool
		for i := range v {
			if m, ok := v[i].(map[string]any); ok {
				output, err := expr.Run(program, Env{Data: m})
				if err != nil {
					return false, err
				}
				rs = rs || output.(bool)
			}
		}
		return rs, nil
	default:
		return false, nil
	}
}

// Package record defines the canonical, format-agnostic view of one parsed
// log line: a map of nginx variable name -> string value, plus typed
// accessors for the fields gonginxlog's filters and stats care about.
package record

import (
	"strconv"
	"strings"
	"time"
)

// Record is one parsed log line. Fields is keyed by bare nginx variable
// name (e.g. "remote_addr", "status"), regardless of what JSON key or
// literal position the value came from in the original log_format.
type Record struct {
	Fields map[string]string
	Raw    string
}

// Get returns the value of variable, or "" if the log_format used doesn't
// carry that field (or it was empty / "-").
func (r *Record) Get(variable string) string {
	v := r.Fields[variable]
	if v == "-" {
		return ""
	}
	return v
}

// timeLayouts are tried in order against $time_local / $time_iso8601.
var timeLayouts = []string{
	"02/Jan/2006:15:04:05 -0700", // $time_local
	time.RFC3339,                 // $time_iso8601
}

// Time parses $time_local or $time_iso8601, whichever is present.
func (r *Record) Time() (time.Time, bool) {
	for _, layout := range timeLayouts {
		for _, variable := range []string{"time_local", "time_iso8601"} {
			v := r.Get(variable)
			if v == "" {
				continue
			}
			if t, err := time.Parse(layout, v); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// StatusCode parses $status.
func (r *Record) StatusCode() (int, bool) {
	v := r.Get("status")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// RemoteAddr returns $remote_addr.
func (r *Record) RemoteAddr() string {
	return r.Get("remote_addr")
}

// Country returns $geoip_country_code (legacy ngx_http_geoip_module) or
// $geoip2_data_country_code (ngx_http_geoip2_module), whichever is present.
func (r *Record) Country() string {
	for _, variable := range []string{"geoip_country_code", "geoip2_data_country_code"} {
		if v := r.Get(variable); v != "" {
			return v
		}
	}
	return ""
}

// Request returns the raw $request line, e.g. `GET /foo?x=1 HTTP/1.1`.
func (r *Record) Request() string {
	return r.Get("request")
}

// Method returns the HTTP method parsed out of $request.
func (r *Record) Method() string {
	parts := strings.Fields(r.Request())
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// Path returns the request target (path + query) parsed out of $request.
func (r *Record) Path() string {
	parts := strings.Fields(r.Request())
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// PathWithoutQuery returns Path() with the query string - everything
// from the first '?' onward - removed. High-cardinality query
// parameters (numeric IDs, tokens, ...) are the usual reason a raw
// path never repeats often enough to be a useful "top paths" entry;
// trimming them is a cheap default that doesn't need per-site
// configuration the way --routes-file's regex grouping does.
func (r *Record) PathWithoutQuery() string {
	p := r.Path()
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// RequestTime parses $request_time (seconds, fractional).
func (r *Record) RequestTime() (float64, bool) {
	return r.parseFloatField("request_time")
}

// UpstreamResponseTime parses $upstream_response_time. Upstream time can be
// a comma-separated list when a request was proxied through multiple
// upstreams (retries); this returns the sum, which is what nginx logs as
// the total time spent waiting on upstreams.
func (r *Record) UpstreamResponseTime() (float64, bool) {
	v := r.Get("upstream_response_time")
	if v == "" {
		return 0, false
	}
	var sum float64
	var any bool
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "-" {
			continue
		}
		f, err := strconv.ParseFloat(part, 64)
		if err != nil {
			continue
		}
		sum += f
		any = true
	}
	return sum, any
}

// UpstreamAddr returns $upstream_addr - the address that actually
// answered, when nginx tried multiple upstreams for one request (a
// retry produces a comma-separated list; the last entry is the one
// whose response the client received). Returns "" when there was no
// upstream at all (served from cache or a static file).
func (r *Record) UpstreamAddr() string {
	return lastCommaValue(r.Get("upstream_addr"))
}

// UpstreamStatus parses $upstream_status the same way: the last
// comma-separated value, i.e. the status code the upstream side
// actually returned for the response the client got (nginx's own
// $status can differ, e.g. when nginx maps a 502 to a custom error
// page).
func (r *Record) UpstreamStatus() (int, bool) {
	v := lastCommaValue(r.Get("upstream_status"))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// UpstreamResponseTimeLast returns just the last upstream's own
// response time - as opposed to UpstreamResponseTime's sum across
// every retry - so a per-upstream average isn't diluted by time spent
// waiting on a *different* upstream earlier in the same request.
func (r *Record) UpstreamResponseTimeLast() (float64, bool) {
	v := lastCommaValue(r.Get("upstream_response_time"))
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// lastCommaValue returns the last comma-separated, trimmed entry of v
// (nginx's convention for multi-upstream fields on a retried request),
// normalizing an empty or "-" result to "".
func lastCommaValue(v string) string {
	if v == "" {
		return ""
	}
	parts := strings.Split(v, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "-" {
		return ""
	}
	return last
}

// BytesSent returns $bytes_sent, falling back to $body_bytes_sent.
func (r *Record) BytesSent() (int64, bool) {
	for _, variable := range []string{"bytes_sent", "body_bytes_sent"} {
		v := r.Get(variable)
		if v == "" {
			continue
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// UserAgent returns $http_user_agent.
func (r *Record) UserAgent() string {
	return r.Get("http_user_agent")
}

// Referer returns $http_referer.
func (r *Record) Referer() string {
	return r.Get("http_referer")
}

func (r *Record) parseFloatField(variable string) (float64, bool) {
	v := r.Get(variable)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

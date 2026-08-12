package format

// defaultTemplate mirrors the main_json log_format from the project brief:
//
//	log_format main_json escape=json '{'
//	        '"remote_addr":"$remote_addr",'
//	        ...
//	'}';
const defaultTemplate = `{` +
	`"remote_addr":"$remote_addr",` +
	`"country":"$geoip_country_code",` +
	`"remote_user":"$remote_user",` +
	`"time_local":"$time_local",` +
	`"ssl_protocol":"$ssl_protocol",` +
	`"host":"$host",` +
	`"request":"$request",` +
	`"status":"$status",` +
	`"bytes_sent":"$bytes_sent",` +
	`"http_referer":"$http_referer",` +
	`"http_user_agent":"$http_user_agent",` +
	`"http_cookie":"$http_cookie",` +
	`"http_x_forwarded_for":"$http_x_forwarded_for",` +
	`"request_time":"$request_time",` +
	`"request_length":"$request_length",` +
	`"upstream_addr":"$upstream_addr",` +
	`"upstream_response_time":"$upstream_response_time",` +
	`"upstream_status":"$upstream_status",` +
	`"upstream_cache_status":"$upstream_cache_status",` +
	`"x-request-id":"$request_id"` +
	`}`

// DefaultName is the log_format name used when none is given on the CLI.
const DefaultName = "main_json"

// Default returns the Spec for the built-in main_json format used when the
// caller doesn't point gonginxlog at an nginx.conf.
func Default() *Spec {
	spec, err := ParseTemplate(DefaultName, defaultTemplate, true)
	if err != nil {
		// The default template is a compile-time constant we control; if
		// this ever fails it's a bug in ParseTemplate, not user input.
		panic(err)
	}
	return spec
}

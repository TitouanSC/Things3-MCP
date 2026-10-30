package local

import (
	"encoding/json"
	"net/url"
	"strings"
)

// thingsURL builds a things:/// URL. Values are percent-encoded with %20 for
// spaces (Things does not decode '+'), and '&', '=', '#' are always escaped,
// so user content cannot inject extra parameters such as auth-token or id.
type thingsURL struct {
	command string
	params  []string // "key=value" pairs, already encoded, in insertion order
}

func newURL(command string) *thingsURL { return &thingsURL{command: command} }

func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// set adds a parameter. An empty value is sent as is: for update commands it
// clears the field.
func (u *thingsURL) set(key, value string) *thingsURL {
	u.params = append(u.params, key+"="+escape(value))
	return u
}

// setIf adds a parameter only when the value is not empty.
func (u *thingsURL) setIf(key, value string) *thingsURL {
	if value != "" {
		u.set(key, value)
	}
	return u
}

func (u *thingsURL) String() string {
	s := "things:///" + u.command
	if len(u.params) > 0 {
		s += "?" + strings.Join(u.params, "&")
	}
	return s
}

// jsonItem is an object of the Things JSON command.
type jsonItem struct {
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}

func jsonData(items []jsonItem) (string, error) {
	b, err := json.Marshal(items)
	return string(b), err
}

// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * rtjson - routing JSON serialisation.
 *
 * Parses and builds the JSON route descriptors used to drive dynamic
 * routing decisions. Mirrors the kamailio rtjson module.
 */

package rtjson

import (
	"encoding/json"
	"errors"
)

// RTJSONRoute describes a routing decision expressed as JSON.
type RTJSONRoute struct {
	Destinations []string `json:"destinations"`
	Mode         string   `json:"mode"`
	Flags        int      `json:"flags"`
}

// RTJSONModule parses and builds routing JSON.
type RTJSONModule struct{}

// New returns a new RTJSONModule.
func New() *RTJSONModule { return &RTJSONModule{} }

// Parse decodes a JSON string into an RTJSONRoute.
func (m *RTJSONModule) Parse(j string) (*RTJSONRoute, error) {
	if m == nil {
		return nil, errors.New("rtjson: nil module")
	}
	var r RTJSONRoute
	if err := json.Unmarshal([]byte(j), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Build serialises an RTJSONRoute into a JSON string.
func (m *RTJSONModule) Build(route *RTJSONRoute) string {
	if m == nil || route == nil {
		return ""
	}
	data, err := json.Marshal(route)
	if err != nil {
		return ""
	}
	return string(data)
}

package main

import (
	"encoding/json"
	"strings"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func pathNoExt(p string) string         { return strings.TrimSuffix(p, ".jpg") }

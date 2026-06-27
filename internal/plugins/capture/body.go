package capture

import (
	"encoding/base64"
	"encoding/json"
	"mime"
	"strings"
	"unicode/utf8"
)

const (
	BodyEncodingJSON   = "json"
	BodyEncodingText   = "text"
	BodyEncodingBase64 = "base64"
)

func buildBody(data []byte, contentType string) *Body {
	encoding, content := encodeBodyContent(data, contentType)
	return &Body{
		Content:     content,
		Size:        len(data),
		ContentType: contentType,
		Encoding:    encoding,
	}
}

func encodeBodyContent(data []byte, contentType string) (string, any) {
	if isJSONContentType(contentType) {
		var value any
		if err := json.Unmarshal(data, &value); err == nil {
			return BodyEncodingJSON, value
		}
	}
	if isTextContentType(contentType) || (strings.TrimSpace(contentType) == "" && utf8.Valid(data)) {
		return BodyEncodingText, string(data)
	}
	return BodyEncodingBase64, base64.StdEncoding.EncodeToString(data)
}

func isJSONContentType(contentType string) bool {
	mediaType := parseMediaType(contentType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func isTextContentType(contentType string) bool {
	mediaType := parseMediaType(contentType)
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/x-www-form-urlencoded" ||
		mediaType == "application/xml" ||
		strings.HasSuffix(mediaType, "+xml")
}

func parseMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		mediaType = strings.TrimSpace(contentType)
	}
	return strings.ToLower(mediaType)
}

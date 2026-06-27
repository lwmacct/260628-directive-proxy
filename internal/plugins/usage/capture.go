package usage

import (
	"io"

	"github.com/lwmacct/260628-llm-relay-dproxy/pkg/jsoncapture"
)

func newCaptureOptions(mode Mode, fields []string) jsoncapture.Options {
	return jsoncapture.Options{
		ObjectPath: []string{"response"},
		Mode:       jsoncapture.Mode(mode),
		Fields:     append([]string(nil), fields...),
	}
}

func extractCompletedUsage(r io.Reader) (jsoncapture.Result, bool, error) {
	result, err := jsoncapture.Capture(r, newCaptureOptions(ModeInclude, []string{"usage"}))
	if err != nil {
		return jsoncapture.Result{}, false, err
	}
	_, ok := result.Fields["usage"]
	return result, ok, nil
}

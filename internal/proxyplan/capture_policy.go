package proxyplan

type CapturePolicy struct {
	Configured       bool
	RequestHeaders   bool
	ResponseHeaders  bool
	RequestBody      bool
	ResponseBody     bool
	StreamEvents     bool
	StreamEventTypes []string
}

func DefaultCapturePolicy() CapturePolicy {
	return CapturePolicy{
		Configured: true,
	}
}

func (p CapturePolicy) WithDefaults() CapturePolicy {
	if !p.Configured {
		p.StreamEvents = false
		p.StreamEventTypes = nil
		return p
	}
	if p.RequestBody {
		p.RequestHeaders = true
	}
	if p.ResponseBody {
		p.ResponseHeaders = true
	}
	p.StreamEventTypes = append([]string(nil), p.StreamEventTypes...)
	return p
}

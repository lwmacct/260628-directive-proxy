package eventbus

func CloneRuntime(in Runtime) Runtime {
	out := Runtime{
		IncomingRemoteAddr: in.IncomingRemoteAddr,
		ClientRequestID:    in.ClientRequestID,
	}
	if len(in.Headers) > 0 {
		out.Headers = make(map[string][]string, len(in.Headers))
		for k, values := range in.Headers {
			out.Headers[k] = append([]string(nil), values...)
		}
	}
	return out
}
